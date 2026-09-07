package flow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// placeholderRe matches {{variableName}} placeholders in content.
var placeholderRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Interpolate replaces all {{variableName}} placeholders in text with
// the corresponding value from the variables map. Unknown placeholders
// are left as-is (or removed if no value provided).
func Interpolate(text string, variables map[string]any) string {
	if len(variables) == 0 {
		return text
	}
	return placeholderRe.ReplaceAllStringFunc(text, func(match string) string {
		varName := match[2 : len(match)-2] // strip {{ and }}
		if val, ok := variables[varName]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match // leave unknown placeholders unchanged
	})
}

// InterpolateContent applies variable interpolation to all string fields
// of a MessageContent struct.
func InterpolateContent(content *MessageContent, variables map[string]any) *MessageContent {
	if content == nil {
		return nil
	}
	return &MessageContent{
		Body:    Interpolate(content.Body, variables),
		Subject: Interpolate(content.Subject, variables),
		Title:   Interpolate(content.Title, variables),
		HTML:    Interpolate(content.HTML, variables),
		Data:    content.Data, // data payload is not interpolated
	}
}

// MissingVariables checks which required variables are missing from the
// provided variables map. Returns a sorted list of missing variable names.
func MissingVariables(required []string, variables map[string]any) []string {
	var missing []string
	for _, name := range required {
		if _, ok := variables[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// MergeVariables merges two variable maps, with channelVariables taking
// precedence over base variables.
func MergeVariables(base, channelVars map[string]any) map[string]any {
	if len(base) == 0 && len(channelVars) == 0 {
		return nil
	}
	merged := make(map[string]any, len(base)+len(channelVars))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range channelVars {
		merged[k] = v
	}
	return merged
}

// RenderPositional renders positional parameters into the template body.
// Each param in paramOrder is mapped to its value in order, producing
// {{1}}, {{2}}, ... style placeholders for WhatsApp-style templates.
func RenderPositional(body string, paramOrder []string, variables map[string]any) string {
	for i, name := range paramOrder {
		key := fmt.Sprintf("{{%d}}", i+1)
		val := fmt.Sprintf("%v", variables[name])
		body = strings.ReplaceAll(body, key, val)
	}
	return body
}
