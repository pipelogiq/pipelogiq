package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestPipelineFilterHTTPBoundaryRejectsMalformedFilters(t *testing.T) {
	// An empty application scope avoids database access. Validation still runs
	// before this shortcut, exercising the actual HTTP query boundary.
	server := &Server{}
	for _, parameter := range []string{"contextFilters", "labels"} {
		for _, raw := range []string{
			`not-json`, `null`, `[]`, `"text"`, `{}`, `{"value":"x"}`,
			`{"key":" " ,"value":"x"}`, `{"key":"order"}`,
			`{"key":"order","value":null}`, `{"key":"order","value":42}`,
			`{"key":"order","value":{}}`, `{"key":false,"value":"x"}`,
		} {
			t.Run(parameter+"/"+raw, func(t *testing.T) {
				query := url.Values{parameter: []string{raw}}
				request := httptest.NewRequest(http.MethodGet, "/pipelines?"+query.Encode(), nil)
				recorder := httptest.NewRecorder()
				server.handleGetPipelines(recorder, request)
				if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), parameter) {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
			})
		}
	}
}

func TestPipelinePaginationRejectsInvalidAndOverflowingOffsets(t *testing.T) {
	server := &Server{}
	for _, query := range []string{
		"pageNumber=0", "pageNumber=-1", "pageNumber=invalid", "pageSize=0", "pageSize=-1",
		"pageSize=999999999999999999999999999999", "pageNumber=" + strconv.Itoa(int(^uint(0)>>1)),
		"pageNumber=" + strconv.Itoa(int(^uint(0)>>1)) + "&pageSize=1000000",
	} {
		t.Run(query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.handleGetPipelines(recorder, httptest.NewRequest(http.MethodGet, "/pipelines?"+query, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	for _, query := range []string{"pageNumber=1&pageSize=1", "pageNumber=2&pageSize=1000000"} {
		recorder := httptest.NewRecorder()
		server.handleGetPipelines(recorder, httptest.NewRequest(http.MethodGet, "/pipelines?"+query, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("valid capped pagination rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestPipelineFilterHTTPBoundaryAcceptsExplicitStringsAndLimitsCombinedCount(t *testing.T) {
	server := &Server{}
	for _, raw := range []string{`{"key":"order","value":""}`, `{"key":"order","value":"50%_off\\quote\""}`} {
		query := url.Values{"contextFilters": []string{raw}, "labels": []string{raw}}
		recorder := httptest.NewRecorder()
		server.handleGetPipelines(recorder, httptest.NewRequest(http.MethodGet, "/pipelines?"+query.Encode(), nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("valid filter rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	query := url.Values{}
	for i := 0; i < 100; i++ {
		query.Add("tags", "one")
	}
	query.Add("labels", `{"key":"order","value":"x"}`)
	recorder := httptest.NewRecorder()
	server.handleGetPipelines(recorder, httptest.NewRequest(http.MethodGet, "/pipelines?"+query.Encode(), nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "too many filters") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
