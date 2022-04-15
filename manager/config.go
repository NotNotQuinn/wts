package manager

import (
	"errors"
	"fmt"
	"os"

	"github.com/itchyny/gojq"
	"github.com/notnotquinn/wts"
	"gopkg.in/yaml.v3"
)

// Config is the configuration for a Manager
type Config struct {
	// The URL for the manager to use as its base
	BaseURL string `yaml:"baseURL"`
	// The port to expose itself on
	HubPort int `yaml:"port"`
	// The default value for all variables
	InitialVars map[string]string `yaml:"vars"`
	// Different rules for configuring logic
	Rules map[string]ConfRule `yaml:"rules"`
}

// ConfRule is a rule that can be triggered by triggers, and has variables local to itself.
//
// When a rule is triggered all of the rule's actions are also triggered.
type ConfRule struct {
	Triggers map[string]ConfTrigger `yaml:"triggers"`
	Actions  map[string]ConfAction  `yaml:"actions"`
}

// ConfTrigger is a single trigger for a rule.
//
// When a trigger is triggered, it can optionally set certain variables.
type ConfTrigger struct {
	Event      *string                         `yaml:"event"`
	ModifyVars map[string]ConfVariableModifier `yaml:"modify-vars"`
}

// ConfVariableModifier modifies a variable when it is triggered to either as part of an action
// or as part of a trigger.
type ConfVariableModifier struct {
	JSONQuery *string `yaml:"set"`
	Reset     *bool   `yaml:"reset"`
}

// ConfAction is a single action that can be taken when a rule is triggered.
//
// The action itself may have conditions that must be met for it to trigger,
// other than the rule its a part of triggering.
type ConfAction struct {
	TriggerCondition *ConfTriggerCondition           `yaml:"if"`
	ModifyVars       map[string]ConfVariableModifier `yaml:"modify-vars"`
	EventJQ          string                          `yaml:"event"`
	DataJQ           string                          `yaml:"data"`
}

// ConfTriggerCondition specifies a condition.
//
// AND and OR are mutually exclusive
type ConfTriggerCondition struct {
	// A JSON query to get a value from
	JSONQuery *string `yaml:"jq"`

	// Checks if the value is this string
	Is *string `yaml:"is"`

	// Conditions to be logically ORed with this one
	OR []*ConfTriggerCondition `yaml:"or"`
	// Conditions to be logically ANDed with this one
	AND []*ConfTriggerCondition `yaml:"and"`
}

func (c *Config) Load(configFile string) []error {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return []error{err}
	}

	err = yaml.Unmarshal(content, c)
	if err != nil {
		return []error{err}
	}

	return ValidateConfig(c)
}

// ValidateConfig checks if a config is valid.
func ValidateConfig(c *Config) []error {
	return c.validate()
}

// validate checks the Config is valid
func (c *Config) validate() (errs []error) {
	var err error
	globalVars := map[string]bool{}

	for varName := range c.InitialVars {
		var hadErr bool

		if varName[0] == '_' {
			hadErr = true
			err = fmt.Errorf("variable name must not start with an underscore: %q", varName)
			errs = append(errs, err)
		}

		if globalVars[varName] {
			hadErr = true
			err = fmt.Errorf("global variable %q redeclared", varName)
			errs = append(errs, err)
		}

		if !hadErr {
			globalVars[varName] = true
		}
	}

	for ruleName, rule := range c.Rules {
		validationErrors := rule.validate(globalVars)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("rules: %q: %w", ruleName, err2)
			errs = append(errs, err)
		}
	}

	return errs
}

// validate checks the ConfRule is valid
func (r *ConfRule) validate(vars map[string]bool) (errs []error) {
	var err error

	triggers := map[string]bool{}
	for triggerName, trigger := range r.Triggers {
		validationErrors := trigger.validate(vars)

		hadErr := false

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("triggers: %q: %w", triggerName, err2)
			errs = append(errs, err)
			hadErr = true
		}

		if !hadErr {
			triggers[triggerName] = true
		}
	}

	for actionName, action := range r.Actions {
		validationErrors := action.validate(vars, triggers)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("actions: %q: %w", actionName, err2)
			errs = append(errs, err)
		}
	}

	return errs
}

// validate checks the ConfAction is valid
func (a *ConfAction) validate(vars, triggers map[string]bool) (errs []error) {
	var err error
	for varName, vm := range a.ModifyVars {
		validationErrors := vm.validate(varName, vars)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("modify-vars: %q: %w", varName, err2)
			errs = append(errs, err)
		}
	}

	if a.TriggerCondition != nil {
		validationErrors := a.TriggerCondition.validate(vars, triggers)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("if: %w", err2)
			errs = append(errs, err)
		}
	}

	if a.DataJQ == "" {
		err = fmt.Errorf("'data:' must be set")
		errs = append(errs, err)
	}
	_, err = gojq.Parse(a.DataJQ)
	if err != nil {
		err = fmt.Errorf("data: jq parsing: %w", err)
		errs = append(errs, err)
	}

	if a.EventJQ == "" {
		err = fmt.Errorf("'event:' must be set")
		errs = append(errs, err)
	}
	_, err = gojq.Parse(a.EventJQ)
	if err != nil {
		err = fmt.Errorf("event: jq parsing: %w", err)
		errs = append(errs, err)
	}

	return errs
}

func (t *ConfTriggerCondition) validate(vars map[string]bool, triggers map[string]bool) (errs []error) {
	var err error
	if t == nil {
		return []error{errors.New("empty condition")}
	}

	var hadCondition bool

	if t.AND != nil && t.OR != nil {
		err = errors.New("'and' and 'or' are mutually exclusive")
		errs = append(errs, err)
	}

	if t.AND != nil || t.OR != nil || t.JSONQuery != nil {
		hadCondition = true
	}

	if t.Is != nil {
		hadCondition = true
		if t.JSONQuery == nil {
			err = errors.New("cannot have 'is:' without 'jq:'")
			errs = append(errs, err)
		}
	}

	if !hadCondition {
		err = errors.New("condition is meaningless")
		errs = append(errs, err)
	}

	return
}

// validate checks the ConfTrigger is valid
func (t *ConfTrigger) validate(vars map[string]bool) (errs []error) {
	var err error
	for varName, vm := range t.ModifyVars {
		validationErrors := vm.validate(varName, vars)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("modify-vars: %q: %w", varName, err2)
			errs = append(errs, err)
		}
	}

	err = validateEventString(*t.Event)
	if err != nil {
		err = fmt.Errorf("event: %w", err)
		errs = append(errs, err)
	}

	return errs
}

// validate checks the ConfVariableModifier is valid
func (vm *ConfVariableModifier) validate(varName string, vars map[string]bool) (errs []error) {
	var err error
	if !vars[varName] {
		err = fmt.Errorf("variable %q does not exist", varName)
		errs = append(errs, err)
	}

	if vm.Reset == nil && vm.JSONQuery == nil {
		err = fmt.Errorf("one of 'reset:' or 'set:' must be set")
		errs = append(errs, err)
	}

	if vm.Reset != nil && vm.JSONQuery != nil {
		err = fmt.Errorf("'reset:' and 'set:' are mutually exclusive")
		errs = append(errs, err)
	}

	return errs
}

// validateEventString validates an Event URL is actually an event URL
func validateEventString(eventURL string) (err error) {
	_, _, err = wts.ParseEventURL(eventURL)

	return err
}
