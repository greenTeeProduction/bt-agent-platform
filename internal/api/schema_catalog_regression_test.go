package api

import (
	"fmt"
	"testing"
)

// Dashboard required fields are named, typed contract properties. Descriptions
// belong in Description, not in ObjectSchema's variadic required list.
func TestDashboardRequiredFieldsHaveDeclaredProperties(t *testing.T) {
	var inspect func(*Schema, string)
	inspect = func(schema *Schema, location string) {
		if schema == nil {
			return
		}
		for _, name := range schema.Required {
			if schema.Type != "object" || schema.Properties[name] == nil {
				t.Errorf("%s: required field %q has no declared property", location, name)
			}
		}
		for name, child := range schema.Properties {
			inspect(child, location+"."+name)
		}
		inspect(schema.Items, location+"[]")
	}
	for _, route := range DashboardRoutes() {
		location := string(route.Method) + " " + route.Path
		inspect(route.RequestBody, location+" request")
		for _, parameter := range route.Parameters {
			inspect(parameter.Schema, location+" parameter "+parameter.Name)
		}
		for _, response := range route.Responses {
			inspect(response.Schema, fmt.Sprintf("%s response %d", location, response.StatusCode))
		}
	}
}
