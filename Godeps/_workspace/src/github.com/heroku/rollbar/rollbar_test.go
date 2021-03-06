package rollbar

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
)

type CustomError struct {
	s string
}

func (e *CustomError) Error() string {
	return e.s
}

func testErrorStack(s string) {
	testErrorStack2(s)
}

func testErrorStack2(s string) {
	Error("error", errors.New(s))
}

func testErrorStackWithSkip(s string) {
	testErrorStackWithSkip2(s)
}

func testErrorStackWithSkip2(s string) {
	ErrorWithStackSkip("error", errors.New(s), 2)
}

func TestErrorClass(t *testing.T) {
	errors := map[string]error{
		"{508e076d}":          fmt.Errorf("Something is broken!"),
		"rollbar.CustomError": &CustomError{"Terrible mistakes were made."},
	}

	for expected, err := range errors {
		if errorClass(err) != expected {
			t.Error("Got:", errorClass(err), "Expected:", expected)
		}
	}
}

func TestEverything(t *testing.T) {
	Token = os.Getenv("TOKEN")
	Environment = "test"

	Error("critical", errors.New("Normal critical error"))
	Error("error", &CustomError{"This is a custom error"})

	testErrorStack("This error should have a nice stacktrace")
	testErrorStackWithSkip("This error should have a skipped stacktrace")

	done := make(chan bool)
	go func() {
		testErrorStack("I'm in a goroutine")
		done <- true
	}()
	<-done

	Message("error", "This is an error message")
	Message("info", "And this is an info message")

	// If you don't see the message sent on line 65 in Rollbar, that means this
	// is broken:
	Wait()
}

func TestErrorRequest(t *testing.T) {
	r, _ := http.NewRequest("GET", "http://foo.com/somethere?param1=true", nil)
	r.RemoteAddr = "1.1.1.1:123"

	object := errorRequest(r)

	if object["url"] != "http://foo.com/somethere?param1=true" {
		t.Errorf("wrong url, got %v", object["url"])
	}

	if object["method"] != "GET" {
		t.Errorf("wrong method, got %v", object["method"])
	}

	if object["query_string"] != "param1=true" {
		t.Errorf("wrong id, got %v", object["query_string"])
	}
}

func TestFilterParams(t *testing.T) {
	values := map[string][]string{
		"password":     []string{"one"},
		"ok":           []string{"one"},
		"access_token": []string{"one"},
	}

	clean := filterParams(FilterFields, values)
	if clean["password"][0] != FILTERED {
		t.Error("should filter password parameter")
	}

	if clean["ok"][0] == FILTERED {
		t.Error("should keep ok parameter")
	}

	if clean["access_token"][0] != FILTERED {
		t.Error("should filter access_token parameter")
	}
}

func TestFlattenValues(t *testing.T) {
	values := map[string][]string{
		"a": []string{"one"},
		"b": []string{"one", "two"},
	}

	flattened := flattenValues(values)
	if flattened["a"].(string) != "one" {
		t.Error("should flatten single parameter to string")
	}

	if len(flattened["b"].([]string)) != 2 {
		t.Error("should leave multiple parametres as []string")
	}
}

type cs struct {
	error
	cause error
	stack Stack
}

func (cs cs) Cause() error {
	return cs.cause
}

func (cs cs) Stack() Stack {
	return cs.stack
}

func TestGetCauseOfStdErr(t *testing.T) {
	if nil != getCause(fmt.Errorf("")) {
		t.Error("cause should be nil for standard error")
	}
}

func TestGetCauseOfCauseStacker(t *testing.T) {
	cause := fmt.Errorf("cause")
	effect := cs{fmt.Errorf("effect"), cause, nil}
	if cause != getCause(effect) {
		t.Error("effect should return cause")
	}
}

func TestGetOrBuildStackOfStdErrWithoutParent(t *testing.T) {
	err := cs{fmt.Errorf(""), nil, BuildStack(0)}
	if nil == getOrBuildStack(err, nil, 0) {
		t.Error("should build stack if parent is not a CauseStacker")
	}
}

func TestGetOrBuildStackOfStdErrWithParent(t *testing.T) {
	cause := fmt.Errorf("cause")
	effect := cs{fmt.Errorf("effect"), cause, BuildStack(0)}
	if 0 != len(getOrBuildStack(cause, effect, 0)) {
		t.Error("should return empty stack of stadard error if parent is CauseStacker")
	}
}

func TestGetOrBuildStackOfCauseStackerWithoutParent(t *testing.T) {
	cause := fmt.Errorf("cause")
	effect := cs{fmt.Errorf("effect"), cause, BuildStack(0)}
	if effect.Stack()[0] != getOrBuildStack(effect, nil, 0)[0] {
		t.Error("should use stack from effect")
	}
}

func TestGetOrBuildStackOfCauseStackerWithParent(t *testing.T) {
	cause := fmt.Errorf("cause")
	effect := cs{fmt.Errorf("effect"), cause, BuildStack(0)}
	effect2 := cs{fmt.Errorf("effect2"), effect, BuildStack(0)}
	if effect.Stack()[0] != getOrBuildStack(effect2, effect, 0)[0] {
		t.Error("should use stack from effect2")
	}
}

func TestErrorBodyWithoutChain(t *testing.T) {
	err := fmt.Errorf("ERR")
	errorBody, fingerprint := errorBody(err, 0)
	if nil != errorBody["trace"] {
		t.Error("should not have trace element")
	}
	if nil == errorBody["trace_chain"] {
		t.Error("should have trace_chain element")
	}
	traces := errorBody["trace_chain"].([]map[string]interface{})
	if 1 != len(traces) {
		t.Error("chain should contain 1 trace")
	}
	if "ERR" != traces[0]["exception"].(map[string]interface{})["message"] {
		t.Error("chain should contain err")
	}
	if "0" == fingerprint {
		t.Error("fingerprint should be auto-generated and non-zero. got: ", fingerprint)
	}
}

func TestErrorBodyWithChain(t *testing.T) {
	cause := fmt.Errorf("cause")
	effect := cs{fmt.Errorf("effect1"), cause, BuildStack(0)}
	effect2 := cs{fmt.Errorf("effect2"), effect, BuildStack(0)}
	errorBody, fingerprint := errorBody(effect2, 0)
	if nil != errorBody["trace"] {
		t.Error("should not have trace element")
	}
	if nil == errorBody["trace_chain"] {
		t.Error("should have trace_chain element")
	}
	traces := errorBody["trace_chain"].([]map[string]interface{})
	if 3 != len(traces) {
		t.Error("chain should contain 3 traces")
	}
	if "effect2" != traces[0]["exception"].(map[string]interface{})["message"] {
		t.Error("chain should contain effect2 first")
	}
	if "effect1" != traces[1]["exception"].(map[string]interface{})["message"] {
		t.Error("chain should contain effect1 second")
	}
	if "cause" != traces[2]["exception"].(map[string]interface{})["message"] {
		t.Error("chain should contain cause third")
	}
	if effect2.Stack().Fingerprint()+effect.Stack().Fingerprint()+"0" != fingerprint {
		t.Error("fingerprint should be concatination of fingerprints in chain. got: ", fingerprint)
	}
}
