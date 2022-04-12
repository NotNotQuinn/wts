/*
 */
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/akrennmair/slice"
	"github.com/itchyny/gojq"
	"github.com/notnotquinn/go-websub"
	"github.com/notnotquinn/wts"
)

var log = websub.Logger()

// Manager acts as the Hub for multiple Nodes that communicate to eachother,
// and can intercept all communications and events broadcasted between the nodes
// to trigger other events.
type Manager struct {
	*wts.Node
	*websub.Hub
	mux        *http.ServeMux
	Config     *Config
	GlobalVars map[string]string
	Vars       map[string]map[string]string
	// Mutex for variable maps
	mu *sync.RWMutex
}

// NewManager creates a new manager based on the config file.
func NewManager(configFile string) (*Manager, []error) {
	conf := &Config{}
	errs := conf.Load(configFile)
	if len(errs) != 0 {
		return nil, errs
	}

	var globalVars = map[string]string{}
	var vars = map[string]map[string]string{}

	for k, v := range conf.Vars {
		globalVars[k] = v
	}

	for k, r := range conf.Rules {
		vars[k] = map[string]string{}
		for k2, v := range r.Vars {
			vars[k][k2] = v
		}
	}

	h := websub.NewHub(conf.BaseURL,
		websub.HubAllowPostBodyAsContent(true),
		websub.HubExposeTopics(true),
		websub.HubWithHashFunction("sha256"),
		websub.HubWithUserAgent("wts-manager-hub"),
	)

	n := wts.NewNode(h.HubURL()+"/p/", h.HubURL(),
		wts.WithPublisherOptions(
			websub.PublisherAdvertiseInvalidTopics(true),
			websub.PublisherWithPostBodyAsContent(true),
		),
	)

	m := &Manager{
		Hub:        h,
		Node:       n,
		mux:        http.NewServeMux(),
		Config:     conf,
		GlobalVars: globalVars,
		Vars:       vars,
		mu:         &sync.RWMutex{},
	}

	m.mux.Handle("/", h)
	m.mux.Handle("/p/", n)

	m.registerSniffers()

	return m, nil
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

func (m *Manager) getSniffedTopics() (topics []string) {
	for _, r := range m.Config.Rules {
		for _, t := range r.Triggers {
			topics = append(topics, *t.Event)
		}
	}

	return
}

func (m *Manager) registerSniffers() {
	for _, topic := range m.getSniffedTopics() {
		topic = strings.TrimSuffix(topic, "/")
		m.AddSniffer(topic, m.sniffer)
		m.AddSniffer(topic+"/", m.sniffer)
	}

	count := 0
	countMu := sync.Mutex{}
	m.AddSniffer("", func(topic, contentType string, body io.Reader) {
		var currentCount int

		countMu.Lock()
		count++
		currentCount = count
		countMu.Unlock()

		fmt.Printf("Publish #%04d: %s\n", currentCount, topic)
	})
}

func (m *Manager) sniffer(topic, contentType string, bodyReader io.Reader) {

	if contentType != wts.PayloadContentType {
		return
	}

	topic = strings.TrimSuffix(topic, "/")

	body, err := io.ReadAll(bodyReader)
	if err != nil {
		fmt.Println("error reading body in m.sniffer!!: ", err)
		return
	}

	for ruleName, rule := range m.Config.Rules {
		for triggerName, trigger := range rule.Triggers {
			if trigger.Event != nil && strings.TrimSuffix(*trigger.Event, "/") == topic {
				errs := rule.Triggered(&TriggerContext{
					ruleName:    ruleName,
					rule:        rule,
					triggerName: triggerName,
					trigger:     trigger,
					m:           m,
					event:       &topic,
					body:        &body,
				})

				if len(errs) > 0 {
					log.Error().Errs("errors", errs).Msg("errors triggering rule")
				}
			}
		}
	}
}

type TriggerContext struct {
	ruleName    string
	rule        Rule
	triggerName string
	trigger     Trigger
	m           *Manager
	event       *string // may be nil
	body        *[]byte // may be nil
}

func (r *Rule) Triggered(c *TriggerContext) (errs []error) {
	if c.trigger.ModifyVars != nil {
		for variable, modifer := range c.trigger.ModifyVars {
			err := modifer.Execute(variable, c)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	for _, action := range c.rule.Actions {
		pass, errs2 := action.TriggerCondition.Check(c)
		errs = append(errs, errs2...)
		if len(errs) == 0 && pass {
			errs2 := action.Trigger(c)
			errs = append(errs, errs2...)
		}
	}

	// TOxDO: implement triggering a rule
	// TOxDO: implement checking action conditions
	// TOxDO: implement sending events as actions
	// TODO(later): possibly come up with a different way to get the
	//       body for an outgoing event
	// TODO(doing): expose variables in jq somehow
	// TODO: make the event triggered and other shit reserved variables

	if len(errs) == 0 {
		return nil
	}

	return
}

func (a *Action) Trigger(c *TriggerContext) (errs []error) {
	pass, errs2 := a.TriggerCondition.Check(c)
	errs = append(errs, errs2...)
	if !pass {
		return
	}

	for k, vm := range a.ModifyVars {
		err := vm.Execute(k, c)
		if err != nil {
			errs = append(errs, err)
		}
	}

	var event *string
	if a.DynamicEvent != nil {
		value, err := c.m.JSONQuery(*a.DynamicEvent, c)
		if err != nil {
			errs = append(errs, err)
		}

		v, ok := value.(string)
		if ok {
			err := validateEventString(v)
			if err != nil {
				errs = append(errs, err)
			}

			event = &v
		} else {
			errs = append(errs, errors.New("dynamic event returned non-string type"))
		}
	} else if a.Event != nil {
		event = a.Event
	}

	if event != nil {
		if a.DataQuery != nil {
			val, err := c.m.JSONQuery(*a.DataQuery, c)
			if err != nil {
				errs = append(errs, err)
			} else {
				jsonEncoded, err := json.Marshal(val)
				if err != nil {
					errs = append(errs, err)
				} else {
					go func(m *Manager, topic string, contentType string, content []byte) {
						err := m.Publish(topic, contentType, content)
						if err != nil {
							log.Err(err).Msg("could not publish action event")
						}
					}(c.m, *event, wts.PayloadContentType, jsonEncoded)
				}
			}
		} else {
			err := errors.New("cannot post event without data")
			errs = append(errs, err)
		}
	}

	return
}

// Publish calls m.Node.Publish
func (m *Manager) Publish(topic string, contentType string, content []byte) error {
	return m.Node.Publish(topic, contentType, content)
}

// Check checks the condition on a certain context
func (t *TriggerCondition) Check(c *TriggerContext) (pass bool, errs []error) {
	if t == nil {
		// no condition
		return true, nil
	}

	var conditions []bool

	// Value is a value to check by the "is:" option
	var value *string
	if t.JSONQuery != nil {

		val, err := c.m.JSONQuery(*t.JSONQuery, c)
		if err != nil {
			errs = append(errs, err)
		} else {
			str := fmt.Sprint(val)
			value = &str
		}
	}

	if t.Variable != nil {
		val, err := c.m.Var(c.ruleName, *t.Variable)
		if err != nil {
			errs = append(errs, err)
		} else {
			value = &val
		}
	}

	if t.Is != nil {
		if value != nil {
			conditions = append(conditions, (*t.Is) == (*value))
		} else {
			conditions = append(conditions, false)
		}
	}

	if t.EventIs != nil {
		if c.event != nil {
			conditions = append(conditions, *t.EventIs == *c.event)
		} else {
			conditions = append(conditions, false)
		}
	}

	if t.TriggeredBy != nil {
		conditions = append(conditions, *t.TriggeredBy == c.triggerName)
	}

	if len(conditions) == 1 {
		pass = conditions[0]
	} else {
		pass = slice.Reduce(conditions,
			func(t1 bool, t2 bool) bool {
				return t1 && t2
			},
		)
	}

	if t.OR != nil {
		for _, tc2 := range t.OR {
			pass2, errs2 := tc2.Check(c)
			errs = append(errs, errs2...)
			pass = pass || pass2
		}
	} else if t.AND != nil {
		for _, tc2 := range t.AND {
			pass2, errs2 := tc2.Check(c)
			errs = append(errs, errs2...)
			pass = pass && pass2
		}
	}

	if len(errs) > 0 {
		pass = false
	}

	return
}

// SetVar sets a variable, checking if it exists on the global context first.
// If the var does not exist in both global and rule contexts, an error is returned.
func (m *Manager) SetVar(rule, name, value string) error {
	m.mu.RLock()

	if _, ok := m.GlobalVars[name]; ok {
		m.mu.RUnlock()
		m.mu.Lock()
		m.GlobalVars[name] = value
		m.mu.Unlock()
		return nil
	} else if m.Vars[rule] != nil {
		if _, ok := m.Vars[rule][name]; ok {
			m.mu.RUnlock()
			m.mu.Lock()
			m.Vars[rule][name] = value
			m.mu.Unlock()
			return nil
		}
	}

	m.mu.RUnlock()
	return fmt.Errorf("m.SetVar: variable %q does not exist", name)
}

// Var gets a variable, checking if it exists on the global context first.
// If the var does not exist in both global and rule contexts, an error is returned.
func (m *Manager) Var(rule, name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.GlobalVars[name]; ok {
		return m.GlobalVars[name], nil
	} else if m.Vars[rule] != nil {
		if _, ok := m.Vars[rule][name]; ok {
			return m.Vars[rule][name], nil
		}
	}

	return "", fmt.Errorf("m.Var: variable %q does not exist", name)
}

// VarDefault returns the default value for variable
func (m *Manager) VarDefault(rule, name string) (string, error) {
	if _, ok := m.Config.Vars[name]; ok {
		return m.Config.Vars[name], nil
	} else if _, ok := m.Config.Rules[rule]; ok {
		if _, ok := m.Config.Rules[rule].Vars[name]; ok {
			return m.Config.Rules[rule].Vars[name], nil
		}
	}

	return "", fmt.Errorf("m.VarDefault: variable %q does not exist", name)
}

// ResetVar resets a variable to its default value
func (m *Manager) ResetVar(rule, name string) error {
	defaultValue, err := m.VarDefault(rule, name)
	if err != nil {
		return err
	}

	return m.SetVar(rule, name, defaultValue)
}

func (vm *VariableModifier) Execute(variableName string, c *TriggerContext) error {
	var value string

	// calculate/get value
	if vm.SetLiteral != nil {
		value = *vm.SetLiteral
	} else if vm.JSONQuery != nil {
		jqResult, err := c.m.JSONQuery(*vm.JSONQuery, c)
		if err != nil {
			return err
		}

		value = fmt.Sprint(jqResult)
	} else if vm.Reset != nil {
		if *vm.Reset {
			err := c.m.ResetVar(c.ruleName, variableName)
			if err != nil {
				return err
			}
		}
	} else {
		return errors.New("invalid variable modifier struct")
	}

	// set variable
	err := c.m.SetVar(c.ruleName, variableName, value)
	if err != nil {
		return err
	}

	return nil
}

// JSONQuery performs a gojq query on the body, with the json query.
func (m *Manager) JSONQuery(jsonQuery string, c *TriggerContext) (any, error) {
	query, err := gojq.Parse(jsonQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid gojq query: %w", err)
	}

	if c.body == nil {
		return nil, errors.New("json query started for trigger context with no body")
	}

	var event any
	err = json.Unmarshal(*c.body, &event)

	if err != nil {
		return nil, err
	}

	// Get variables within scope
	vars := map[string]string{}
	c.m.mu.RLock()
	for k, v := range c.m.GlobalVars {
		vars[k] = v
	}
	for k, v := range c.m.Vars[c.ruleName] {
		vars[k] = v
	}
	c.m.mu.RUnlock()

	// Add built-in variables
	vars["_rule"] = c.ruleName
	vars["_trigger"] = c.triggerName
	if c.event != nil {
		vars["_event"] = *c.event
	} else {
		vars["_event"] = ""
	}

	// Split variables into names/values
	varNames := make([]string, 0, len(vars))
	for k := range vars {
		varNames = append(varNames, "$"+k)
	}
	varValues := make([]interface{}, 0, len(vars)) // values need to be interface array...
	for _, v := range vars {
		varValues = append(varValues, v)
	}

	// compile/execute with variables
	code, err := gojq.Compile(query, gojq.WithVariables(varNames))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(m.Config.JQTimeoutMS)*time.Millisecond,
	)
	defer cancel()

	iter := code.RunWithContext(ctx, event, varValues...)

	var queryValue interface{}

	var count = 1 // already iterated once
	val, more := iter.Next()

	for ; more; val, more = iter.Next() {
		if count >= m.Config.JQIterationLimit {
			return nil, fmt.Errorf("json query exceeded iteration limit (%d)", m.Config.JQIterationLimit)
		}

		if val != nil {
			queryValue = val
		}

		count++
	}

	return queryValue, nil
}
