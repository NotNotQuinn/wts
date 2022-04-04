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
	BaseURL     string            `yaml:"baseURL"`
	HubPort     int               `yaml:"port"`
	JQTimeoutMS int               `yaml:"jq-timeout-ms"`
	Vars        map[string]string `yaml:"vars"`
	Rules       map[string]Rule   `yaml:"rules"`
}

// Rule is a rule that can be triggered by triggers, and has variables local to itself.
//
// When a rule is triggered all of the rule's actions are also triggered.
type Rule struct {
	Vars     map[string]string  `yaml:"vars"`
	Triggers map[string]Trigger `yaml:"triggers"`
	Actions  map[string]Action  `yaml:"actions"`
}

// Trigger is a single trigger for a rule.
//
// When a trigger is triggered, it can optionally set certain variables.
type Trigger struct {
	Event      *string                     `yaml:"event"`
	ModifyVars map[string]VariableModifier `yaml:"modify-vars"`
}

// VariableModifier modifies a variable when it is triggered to either as part of an action
// or as part of a trigger.
type VariableModifier struct {
	JSONQuery  *string `yaml:"jq"`
	SetLiteral *string `yaml:"set"`
	Reset      *bool   `yaml:"reset"`
}

// Action is a single action that can be taken when a rule is triggered.
//
// The action itself may have conditions that must be met for it to trigger,
// other than the rule its a part of triggering.
type Action struct {
	TriggerCondition *TriggerCondition           `yaml:"if"`
	Event            *string                     `yaml:"event"`
	ModifyVars       map[string]VariableModifier `yaml:"modify-vars"`
	DataQuery        *string                     `yaml:"data-jq"`
}

// TriggerCondition specifies a condition.
//
// Multiple conditions in the same structure are ANDed together.
//
// Fields grouped together are mutually exclusive, and only one should be set.
type TriggerCondition struct {
	// The Event URL must be this for the action to trigger.
	EventIs *string `yaml:"event-is"`

	// The trigger of this action must go by this name for this action to trigger
	TriggeredBy *string `yaml:"triggered-by"`

	// A JSON query to get a value from
	JSONQuery *string `yaml:"jq"`
	// Variable name to get a value from
	Variable *string `yaml:"var"`

	// Checks if the value is this string
	Is *string `yaml:"is"`

	// Conditions to be logically ORed with this one
	OR []*TriggerCondition `yaml:"or"`
	// Conditions to be logically ANDed with this one
	AND []*TriggerCondition `yaml:"and"`
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

	for varName := range c.Vars {
		if globalVars[varName] {
			err = fmt.Errorf("global variable %q redeclared", varName)
			errs = append(errs, err)
		} else {
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

// validate checks the Rule is valid
func (r *Rule) validate(globalVars map[string]bool) (errs []error) {
	var err error
	ruleVars := map[string]bool{}

	for varName := range r.Vars {
		var hadError bool

		if globalVars[varName] {
			hadError = true
			err = fmt.Errorf("variable shaddows global variable %q", varName)
			errs = append(errs, err)
		}

		if ruleVars[varName] {
			hadError = true
			err = fmt.Errorf("redeclaration of variable %q", varName)
			errs = append(errs, err)
		}

		if !hadError {
			ruleVars[varName] = true
		}
	}

	// append global vars to the rule vars, to simplify
	for k, v := range globalVars {
		ruleVars[k] = v
	}

	triggers := map[string]bool{}

	for triggerName, trigger := range r.Triggers {
		validationErrors := trigger.validate(ruleVars)

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
		validationErrors := action.validate(ruleVars, triggers)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("actions: %q: %w", actionName, err2)
			errs = append(errs, err)
		}
	}

	return errs
}

// validate checks the Action is valid
func (a *Action) validate(vars, triggers map[string]bool) (errs []error) {
	var err error
	err = validateEventString(*a.Event)
	if err != nil {
		err = fmt.Errorf("event: %w", err)
		errs = append(errs, err)
	}

	for varName, vm := range a.ModifyVars {
		validationErrors := vm.validate(varName, vars)

		// Wrap all returned errors
		for _, err2 := range validationErrors {
			err = fmt.Errorf("modify-vars: %q: %w", varName, err2)
			errs = append(errs, err)
		}
	}

	validationErrors := a.TriggerCondition.validate(vars, triggers)

	// Wrap all returned errors
	for _, err2 := range validationErrors {
		err = fmt.Errorf("if: %w", err2)
		errs = append(errs, err)
	}

	if a.DataQuery != nil {
		_, err := gojq.Parse(*a.DataQuery)
		if err != nil {

		}
	}

	return errs
}

func (t *TriggerCondition) validate(vars map[string]bool, triggers map[string]bool) (errs []error) {
	var err error
	if t == nil {
		return []error{errors.New("empty condition")}
	}

	var hadCondition bool
	var hadValue bool

	if t.AND != nil && t.OR != nil {
		err = fmt.Errorf("\"and\" and \"or\" are mutually exclusive")
		errs = append(errs, err)
	}

	if t.JSONQuery != nil && t.Variable != nil {
		err = fmt.Errorf("\"jq\" and \"var\" are mutually exclusive")
		errs = append(errs, err)
	}

	if t.EventIs != nil {
		hadCondition = true
		err = validateEventString(*t.EventIs)
		if err != nil {
			err = fmt.Errorf("event-is: %w", err)
			errs = append(errs, err)
		}
	}
	//
	if t.TriggeredBy != nil {
		hadCondition = true
		if !triggers[*t.TriggeredBy] {
			err = fmt.Errorf("triggered-by: trigger %q does not exist on this rule", *t.TriggeredBy)
			errs = append(errs, err)
		}
	}

	if t.JSONQuery != nil {
		hadValue = true
		_, err = gojq.Parse(*t.JSONQuery)
		if err != nil {
			err = fmt.Errorf("jq: %w", err)
			errs = append(errs, err)
		}
	}

	if t.Variable != nil {
		hadValue = true
		if !vars[*t.Variable] {
			err = fmt.Errorf("var: variable %q does not", *t.Variable)
			errs = append(errs, err)
		}
	}

	if t.OR != nil {
		hadCondition = true
		for i, tc := range t.OR {
			validationErrors := tc.validate(vars, triggers)

			// Wrap all returned errors
			for _, err2 := range validationErrors {
				err = fmt.Errorf("or[%d]: %w", i, err2)
				errs = append(errs, err)
			}
		}
	}

	if t.AND != nil {
		hadCondition = true
		for i, tc := range t.AND {
			validationErrors := tc.validate(vars, triggers)

			// Wrap all returned errors
			for _, err2 := range validationErrors {
				err = fmt.Errorf("and[%d]: %w", i, err2)
				errs = append(errs, err)
			}
		}
	}

	if t.Is != nil {
		hadCondition = true
		if !hadValue {
			err = fmt.Errorf("condition has \"is:\" but no value to check (missing \"var: variable\"?)")
			errs = append(errs, err)
		}
	}

	if !hadCondition {
		err = fmt.Errorf("condition is meaningless")
		if hadValue {
			err = fmt.Errorf("condition is meaningless (missing \"is: value\")")
		}
		errs = append(errs, err)
	}

	return
}

// validate checks the Trigger is valid
func (t *Trigger) validate(vars map[string]bool) (errs []error) {
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

// validate checks the VariableModifier is valid
func (vm *VariableModifier) validate(varName string, vars map[string]bool) (errs []error) {
	var err error
	if !vars[varName] {
		err = fmt.Errorf("variable %q does not exist", varName)
		errs = append(errs, err)
	}

	twoOrMore, none := checkExclusive(
		vm.Reset != nil,
		vm.JSONQuery != nil,
		vm.SetLiteral != nil,
	)

	if none {
		err = fmt.Errorf("one of \"reset\", \"jq\" xor \"set\" must be set")
		errs = append(errs, err)
	}

	if twoOrMore {
		err = fmt.Errorf("\"reset\", \"jq\" and \"set\" are mutually exclusive")
		errs = append(errs, err)
	}

	return errs
}

// validateEventString validates an Event URL is actually an event URL
func validateEventString(eventURL string) (err error) {
	_, _, err = wts.ParseEventURL(eventURL)

	return err
}

// checkExclusive tells you if two or more of the conditions are true, or if none are true
func checkExclusive(conditions ...bool) (twoOrMoreTrue, noneTrue bool) {
	anyTrue := false

	for _, cond := range conditions {
		if cond {
			if anyTrue {
				return true, !anyTrue
			} else {
				anyTrue = true
			}
		}
	}

	return false, !anyTrue
}
