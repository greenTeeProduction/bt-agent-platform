package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
)

func newMutationTestRequest(target string) *http.Request {
	u, err := url.Parse(target)
	if err != nil {
		panic(err)
	}
	body := map[string]string{}
	for key, values := range u.Query() {
		body[key] = values[0]
	}
	u.RawQuery = ""
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	r := httptest.NewRequest(http.MethodPost, u.String(), bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	return r
}
