/*
 */
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/akrennmair/slice"
	"github.com/itchyny/gojq"
	"github.com/notnotquinn/go-websub"
	"github.com/notnotquinn/wts"
)

// Manager acts as the Hub for multiple Nodes that communicate to eachother,
// and can intercept all communications and events broadcasted between the nodes
// to trigger other events.
type Manager struct {
	*websub.Hub
	Config     *Config
	GlobalVars map[string]string
	Vars       map[string]map[string]string
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

	m := &Manager{
		Hub:        websub.NewHub(conf.BaseURL),
		Config:     conf,
		GlobalVars: globalVars,
		Vars:       vars,
	}

	m.registerSniffers()

	return m, nil
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
	topics := m.getSniffedTopics()

	for _, topic := range topics {
		topic = strings.TrimSuffix(topic, "/")
		m.AddSniffer(topic, m.sniffer)
		m.AddSniffer(topic+"/", m.sniffer)
	}
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

				// what do I do with these
				_ = errs

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
	event       *string
	body        *[]byte
}

func (r *Rule) Triggered(c *TriggerContext) (errs []error) {
	if c.trigger.ModifyVars != nil {
		for variable, modifer := range c.trigger.ModifyVars {
			err := modifer.Execute(variable, c)
			errs = append(errs, err)
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
	// TODO: implement sending events as actions
	// TODO: possibly come up with a different way to get the
	//       body for an outgoing event
	// TODO: expose variables in jq somehow
	// TODO: make the event triggered and other shit reserved variables

	return
}

func (a *Action) Trigger(c *TriggerContext) (errs []error) {
	pass, errs2 := a.TriggerCondition.Check(c)
	errs = append(errs, errs2...)
	if !pass {
		return
	}

	for k, vm := range a.ModifyVars {
		errs = append(errs, vm.Execute(k, c))
	}

	// TODO: gojq on a.DataQuery

	go c.m.Publish(*a.Event, wts.PayloadContentType, jsonEncoded)

}

// Check checks the condition on a certain context
func (tc *TriggerCondition) Check(c *TriggerContext) (pass bool, errs []error) {
	var conditions []bool

	// Value is a value to check by the "is:" option
	var value *string
	if tc.JSONQuery != nil {
		// call gojq (make the one in the other function into a method)
		panic("unimplemented")
	}

	if tc.Variable != nil {
		val, err := c.m.Var(c.ruleName, *tc.Variable)
		if err != nil {
			errs = append(errs, err)
		} else {
			value = &val
		}
	}

	if tc.Is != nil {
		if value != nil {
			conditions = append(conditions, *tc.Is == *value)
		} else {
			conditions = append(conditions, false)
		}
	}

	if tc.EventIs != nil {
		if c.event != nil {
			conditions = append(conditions, *tc.EventIs == *c.event)
		} else {
			conditions = append(conditions, false)
		}
	}

	if tc.TriggeredBy != nil {
		conditions = append(conditions, *tc.TriggeredBy == c.triggerName)
	}

	pass = slice.Reduce(conditions,
		func(t1 bool, t2 bool) bool {
			return t1 && t2
		},
	)

	if tc.OR != nil {
		for _, tc2 := range tc.OR {
			pass2, errs2 := tc2.Check(c)
			errs = append(errs, errs2...)
			pass = pass || pass2
		}
	} else if tc.AND != nil {
		for _, tc2 := range tc.AND {
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
	if _, ok := m.GlobalVars[name]; ok {
		m.GlobalVars[name] = value
	} else if m.Vars[name] != nil {
		if _, ok := m.Vars[rule][name]; ok {
			m.Vars[name][name] = value
		}
	}

	return fmt.Errorf("variable %q does not exist", name)
}

// Var gets a variable, checking if it exists on the global context first.
// If the var does not exist in both global and rule contexts, an error is returned.
func (m *Manager) Var(rule, name string) (string, error) {
	if _, ok := m.GlobalVars[name]; ok {
		return m.GlobalVars[name], nil
	} else if m.Vars[name] != nil {
		if _, ok := m.Vars[rule][name]; ok {
			return m.Vars[name][name], nil
		}
	}

	return "", fmt.Errorf("variable %q does not exist", name)
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

	return "", fmt.Errorf("variable %q does not exist", name)
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
		if c.body == nil {
			return errors.New("json query for triggered when no event body")
		}

		query, err := gojq.Parse(*vm.JSONQuery)
		if err != nil {
			return fmt.Errorf("invalid gojq query: %w", err)
		}

		ctx, cancel := context.WithTimeout(
			context.Background(),
			time.Duration(c.m.Config.JQTimeoutMS)*time.Millisecond,
		)
		defer cancel()

		var event any
		err = json.Unmarshal(*c.body, event)

		if err != nil {
			return err
		}

		iter := query.RunWithContext(ctx, event)

		queryValue, more := iter.Next()
		if more {
			return errors.New("json query returned multiple values")
		}

		if queryValue == nil {
			return errors.New("json query returned nil")
		}

		value = fmt.Sprint(queryValue)
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
