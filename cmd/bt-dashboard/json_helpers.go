package main

import (
	"encoding/json"
	"net/http"
)

func encodeJSON(w http.ResponseWriter, v any) error {
	return json.NewEncoder(w).Encode(v)
}
