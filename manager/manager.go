package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/notnotquinn/go-websub"
	"github.com/notnotquinn/wts"
)

var log = websub.Logger()

type Manager struct {
	*websub.Hub
	Node          *wts.Node
	Config        *Config
	VariableState map[string]string
	mux           *http.ServeMux
}

func New(configPath string) (*Manager, []error) {
	c := &Config{}
	if err := c.Load(configPath); err != nil {
		return nil, err
	}

	// Calculate base URLs for node and hub
	trimmedBase := strings.TrimRight(c.BaseURL, "/")

	hubPath := "/"
	nodePath := "/event/"

	hubBase := trimmedBase + hubPath
	nodeBase := trimmedBase + nodePath

	m := &Manager{
		Hub: websub.NewHub(hubBase,
			websub.HubAllowPostBodyAsContent(true),
			websub.HubExposeTopics(true),
			websub.HubWithUserAgent("wts-manager-hub"),
			websub.HubWithHashFunction("sha512"),
		),
		Node:          wts.NewNode(nodeBase, hubBase),
		VariableState: map[string]string{},
		Config:        c,
		mux:           http.NewServeMux(),
	}

	// Store initial values
	for k, v := range m.Config.InitialVars {
		m.VariableState[k] = v
	}

	m.mux.Handle(hubPath, http.StripPrefix(strings.TrimSuffix(hubPath, "/"), m.Hub))
	m.mux.Handle(nodePath, http.StripPrefix(strings.TrimSuffix(nodePath, "/"), m.Node))

	err := m.registerEventHooks()
	if err != nil {
		return nil, []error{err}
	}

	return m, nil
}

func (m *Manager) registerEventHooks() error {
	// find all entities and associated events to subscribe to
	var eventsMap = make(map[string]map[wts.EventType]bool)
	for _, rule := range m.Config.Rules {
		for _, trigger := range rule.Triggers {
			if trigger.Event != nil {
				eventTrimmed := strings.TrimRight(*trigger.Event, "/")
				entityURL, eventType, err := wts.ParseEventURL(eventTrimmed)
				if err != nil {
					return err
				}

				if _, ok := eventsMap[entityURL]; !ok {
					eventsMap[entityURL] = map[wts.EventType]bool{
						eventType: true,
					}
				} else {
					eventsMap[entityURL][eventType] = true
				}
			}
		}
	}

	// subscribe to events through the node
	for entityURL, events := range eventsMap {
		var onRequest, onExecuted, onData wts.OnEventFunc[any] = nil, nil, nil
		if events[wts.Request] {
			onRequest = m.handleEvents
		}
		if events[wts.Executed] {
			onExecuted = m.handleEvents
		}
		if events[wts.Data] {
			onData = m.handleEvents
		}

		if onExecuted != nil || onRequest != nil {
			_, err := wts.AddActorHook(m.Node, entityURL, onRequest, onExecuted)
			if err != nil {
				return err
			}
		}

		if onData != nil {
			_, err := wts.AddEmitterHook(m.Node, entityURL, onData)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *Manager) handleEvents(eventURL string, msg *wts.EventPayload[any]) {
	for ruleName, rule := range m.Config.Rules {
		for triggerName, trigger := range rule.Triggers {
			trimmedEventURL := strings.TrimRight(eventURL, "/")
			if trigger.Event != nil && *trigger.Event == trimmedEventURL {
				err := m.ruleTriggered(&TriggerContext{
					ruleName:    ruleName,
					triggerName: triggerName,
					message:     msg,
				})
				log.Err(err)
			}
		}
	}
}

func (m *Manager) ruleTriggered(tctx *TriggerContext) error {
	if rule, ok := m.Config.Rules[tctx.ruleName]; ok {
		if trigger, ok := rule.Triggers[tctx.triggerName]; ok {
			err := m.applyVariableModifiers(trigger.ModifyVars, tctx)
			if err != nil {
				return err
			}
		}

		for _, action := range rule.Actions {
			cond, err := m.checkTriggerCondition(action.TriggerCondition, tctx)
			if err != nil {
				return err
			} else if cond {
				err := m.actionTriggered(action, tctx)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (m *Manager) applyVariableModifiers(modifiers map[string]ConfVariableModifier, tctx *TriggerContext) error {
	for variableName, modifier := range modifiers {
		if _, ok := m.VariableState[variableName]; !ok {
			return fmt.Errorf("variable %q does not exist", variableName)
		}

		if modifier.JSONQuery != nil {
			value, err := m.doJQ(*modifier.JSONQuery, tctx)
			if err != nil {
				return err
			}

			if str, ok := value.(string); ok {
				m.VariableState[variableName] = str
			} else {
				return fmt.Errorf("expected string but got type %T", value)
			}
		}

		if modifier.Reset != nil && *modifier.Reset {
			m.VariableState[variableName] = m.Config.InitialVars[variableName]
		}
	}

	return nil
}

func (m *Manager) doJQ(jq string, tctx *TriggerContext) (value any, err error) {
	query, err := gojq.Parse(jq)
	if err != nil {
		return nil, err
	}

	// Convert the tctx.message to JSON and back to get it into a state that
	// gojq will accept
	var messageAsAny any
	bytes, err := json.Marshal(tctx.message)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(bytes, &messageAsAny)
	if err != nil {
		return nil, err
	}

	// get variables (must be map to any, but only strings are used)
	variables := map[string]any{
		"$_ruleName":    tctx.ruleName,
		"$_triggerName": tctx.triggerName,
		"$_msg":         messageAsAny,
	}
	for varName, value := range m.VariableState {
		if strings.HasPrefix(varName, "_") {
			return nil, fmt.Errorf("variable must not start with '_'. (variable %q)", varName)
		}

		variables["$"+varName] = value
	}

	// split variables into names/values
	variableNames := make([]string, 0, len(variables))
	variableValues := make([]any, 0, len(variables))

	for name, value := range variables {
		variableNames = append(variableNames, name)
		variableValues = append(variableValues, value)
	}

	// compile with variable names
	code, err := gojq.Compile(query, gojq.WithVariables(variableNames))
	if err != nil {
		return nil, err
	}

	// run with variable values
	iter := code.Run(tctx.message.Data, variableValues...)
	for {
		v, ok := iter.Next()
		if !ok {
			return nil, errors.New("jq returned no value")
		}

		if err, ok := v.(error); ok {
			return nil, err
		}

		if v != nil {
			return v, nil
		}
	}
}

func (m *Manager) checkTriggerCondition(cond *ConfTriggerCondition, tctx *TriggerContext) (bool, error) {
	if cond == nil {
		return true, nil
	}

	if cond.AND != nil && cond.OR != nil {
		return false, errors.New("trigger condition has 'and:' and 'or:' set")
	}

	var condition *bool

	if cond.JSONQuery != nil {
		value, err := m.doJQ(*cond.JSONQuery, tctx)
		if err != nil {
			return false, err
		}

		if cond.Is != nil {
			// looking for string value
			if str, ok := value.(string); ok {
				conditionResult := str == *cond.Is
				condition = &conditionResult
			} else {
				return false, fmt.Errorf("expected string but got type %T", value)
			}
		} else {
			// looking for a boolean value
			if conditionResult, ok := value.(bool); ok {
				condition = &conditionResult
			} else {
				return false, fmt.Errorf("expected bool but got type %T", value)
			}
		}
	}

	var op string

	if cond.AND != nil {
		op = "&&"
	} else if cond.OR != nil {
		op = "||"
	}

	if op == "&&" || op == "||" {
		var andOrOrResult bool

		for i, tc := range cond.AND {
			pass, err := m.checkTriggerCondition(tc, tctx)
			if err != nil {
				return false, err
			}

			if i == 0 {
				andOrOrResult = pass
			} else {
				switch op {
				case "&&":
					andOrOrResult = andOrOrResult && pass
				case "||":
					andOrOrResult = andOrOrResult || pass
				}
			}
		}

		if condition != nil {
			switch op {
			case "&&":
				return *condition && andOrOrResult, nil
			case "||":
				return *condition || andOrOrResult, nil
			}
		} else {
			return andOrOrResult, nil
		}
	}

	if condition != nil {
		return *condition, nil
	} else {
		return false, nil
	}
}

func (m *Manager) actionTriggered(action ConfAction, tctx *TriggerContext) error {
	eventJQResult, err := m.doJQ(action.EventJQ, tctx)
	if err != nil {
		return err
	}

	event, ok := eventJQResult.(string)
	if !ok {
		return fmt.Errorf("expected string got %T for event JQ", eventJQResult)
	}

	data, err := m.doJQ(action.DataJQ, tctx)
	if err != nil {
		return err
	}

	// Modify vars after doing all JQs
	err = m.applyVariableModifiers(action.ModifyVars, tctx)
	if err != nil {
		return err
	}

	err = m.Node.BroadcastAny(event, data)
	if err != nil {
		return err
	}

	return nil
}

// ServeHTTP dispatches the request to the node or the hub accordingly.
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

type TriggerContext struct {
	ruleName    string
	triggerName string
	message     *wts.EventPayload[any]
}
