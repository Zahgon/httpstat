package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestTruthy(t *testing.T) {
	cases := []string{"1", "true", "yes", "on", "TRUE", " True ", "YES"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			got, err := parseBool(c)
			if err != nil {
				t.Fatalf("parseBool(%q) unexpected error: %v", c, err)
			}
			if !got {
				t.Fatalf("parseBool(%q) = false, want true", c)
			}
		})
	}
}

func TestFalsy(t *testing.T) {
	cases := []string{"0", "false", "no", "off", "FALSE", " False ", "NO"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			got, err := parseBool(c)
			if err != nil {
				t.Fatalf("parseBool(%q) unexpected error: %v", c, err)
			}
			if got {
				t.Fatalf("parseBool(%q) = true, want false", c)
			}
		})
	}
}

func TestInvalid(t *testing.T) {
	cases := []string{"", "maybe", "2", "truthy"}
	for _, c := range cases {
		t.Run("invalid_"+c, func(t *testing.T) {
			_, err := parseBool(c)
			if err == nil {
				t.Fatalf("parseBool(%q) expected error, got nil", c)
			}
		})
	}
}

func TestPopArg_WithValueLong(t *testing.T) {
	args := []string{"--format", "json", "url"}
	val, ok := popArgValue(&args, "--format")
	if !ok || val != "json" {
		t.Fatalf("val=%q ok=%v; want json,true", val, ok)
	}
	if !reflect.DeepEqual(args, []string{"url"}) {
		t.Fatalf("args=%v; want [url]", args)
	}
}

func TestPopArg_WithValueShort(t *testing.T) {
	args := []string{"-f", "json", "url"}
	val, ok := popArgValue(&args, "-f")
	if !ok || val != "json" {
		t.Fatalf("val=%q ok=%v; want json,true", val, ok)
	}
	if !reflect.DeepEqual(args, []string{"url"}) {
		t.Fatalf("args=%v; want [url]", args)
	}
}

func TestPopArg_NoValueFlag(t *testing.T) {
	args := []string{"--verbose", "url"}
	got := popArgFlag(&args, "--verbose")
	if !got {
		t.Fatalf("popArgFlag returned false, want true")
	}
	if !reflect.DeepEqual(args, []string{"url"}) {
		t.Fatalf("args=%v; want [url]", args)
	}
}

func TestPopArg_Missing(t *testing.T) {
	args := []string{"url"}
	val, ok := popArgValue(&args, "--format")
	if ok || val != "" {
		t.Fatalf("val=%q ok=%v; want \"\",false", val, ok)
	}
	if !reflect.DeepEqual(args, []string{"url"}) {
		t.Fatalf("args=%v; want [url]", args)
	}
}

func TestPopArg_MissingNoValueFlag(t *testing.T) {
	args := []string{"url"}
	got := popArgFlag(&args, "--verbose")
	if got {
		t.Fatalf("popArgFlag returned true, want false")
	}
}

func TestPopArg_AtEndMissingValue(t *testing.T) {
	args := []string{"url", "--format"}
	val, ok := popArgValue(&args, "--format")
	if ok || val != "" {
		t.Fatalf("val=%q ok=%v; want \"\",false", val, ok)
	}
	if !reflect.DeepEqual(args, []string{"url", "--format"}) {
		t.Fatalf("args=%v; want [url --format] (flag NOT removed)", args)
	}
}

func TestPopArg_MultipleFlags(t *testing.T) {
	args := []string{"--format", "json", "--slo", "total=500", "url"}
	fv, ok := popArgValue(&args, "--format")
	if !ok || fv != "json" {
		t.Fatalf("format val=%q ok=%v; want json,true", fv, ok)
	}
	sv, ok := popArgValue(&args, "--slo")
	if !ok || sv != "total=500" {
		t.Fatalf("slo val=%q ok=%v; want total=500,true", sv, ok)
	}
	if !reflect.DeepEqual(args, []string{"url"}) {
		t.Fatalf("args=%v; want [url]", args)
	}
}

func TestParseSLO_SingleKey(t *testing.T) {
	s, err := parseSLO("total=500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Vals["total"] != 500 || len(s.Keys) != 1 {
		t.Fatalf("got %+v", s)
	}
}

func TestParseSLO_MultipleKeys(t *testing.T) {
	s, err := parseSLO("total=500,connect=100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Vals["total"] != 500 || s.Vals["connect"] != 100 {
		t.Fatalf("got %+v", s)
	}
	if len(s.Keys) != 2 || s.Keys[0] != "total" || s.Keys[1] != "connect" {
		t.Fatalf("keys=%v; want [total connect] preserving input order", s.Keys)
	}
}

func TestParseSLO_AllValidKeys(t *testing.T) {
	s, err := parseSLO("total=1000,connect=200,ttfb=500,dns=50,tls=100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, k := range []string{"total", "connect", "ttfb", "dns", "tls"} {
		if _, ok := s.Vals[k]; !ok {
			t.Fatalf("missing key %s in %+v", k, s.Vals)
		}
	}
}

func TestParseSLO_InvalidKey(t *testing.T) {
	if _, err := parseSLO("bogus=100"); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestParseSLO_InvalidValueNotInt(t *testing.T) {
	if _, err := parseSLO("total=abc"); err == nil {
		t.Fatal("expected error for non-int value")
	}
}

func TestParseSLO_InvalidValueNegative(t *testing.T) {
	if _, err := parseSLO("total=-100"); err == nil {
		t.Fatal("expected error for negative value")
	}
}

func TestParseSLO_InvalidValueZero(t *testing.T) {
	if _, err := parseSLO("total=0"); err == nil {
		t.Fatal("expected error for zero value")
	}
}

func TestParseSLO_MalformedNoEquals(t *testing.T) {
	if _, err := parseSLO("total500"); err == nil {
		t.Fatal("expected error for missing =")
	}
}

func TestParseSLO_EmptyString(t *testing.T) {
	if _, err := parseSLO(""); err == nil {
		t.Fatal("expected error for empty spec")
	}
}

func TestParseSLO_SpacesTrimmed(t *testing.T) {
	s, err := parseSLO(" total = 500 , connect = 100 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Vals["total"] != 500 || s.Vals["connect"] != 100 {
		t.Fatalf("got %+v", s)
	}
}

func makeTimings(overrides map[string]int) map[string]int {
	base := map[string]int{
		"time_namelookup":    5,
		"time_connect":       15,
		"time_pretransfer":   30,
		"time_starttransfer": 80,
		"time_total":         100,
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

func TestCheckSLO_AllPass(t *testing.T) {
	slo := &SLO{Keys: []string{"total", "connect"}, Vals: map[string]int{"total": 200, "connect": 50}}
	r := checkSLO(slo, makeTimings(nil))
	if !r.Pass || len(r.Violations) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckSLO_OneViolation(t *testing.T) {
	slo := &SLO{Keys: []string{"total"}, Vals: map[string]int{"total": 50}}
	r := checkSLO(slo, makeTimings(nil))
	if r.Pass || len(r.Violations) != 1 {
		t.Fatalf("got %+v", r)
	}
	v := r.Violations[0]
	if v.Key != "total" || v.ThresholdMs != 50 || v.ActualMs != 100 {
		t.Fatalf("got violation %+v", v)
	}
}

func TestCheckSLO_MultipleViolations(t *testing.T) {
	slo := &SLO{Keys: []string{"total", "connect", "dns"}, Vals: map[string]int{"total": 50, "connect": 10, "dns": 1}}
	r := checkSLO(slo, makeTimings(nil))
	if r.Pass || len(r.Violations) != 3 {
		t.Fatalf("got %+v", r)
	}
	if r.Violations[0].Key != "total" || r.Violations[1].Key != "connect" || r.Violations[2].Key != "dns" {
		t.Fatalf("violation order wrong: %+v", r.Violations)
	}
}

func TestCheckSLO_ExactlyAtThresholdPasses(t *testing.T) {
	slo := &SLO{Keys: []string{"total"}, Vals: map[string]int{"total": 100}}
	r := checkSLO(slo, makeTimings(nil))
	if !r.Pass {
		t.Fatalf("threshold=100, actual=100 should pass (strict >), got %+v", r)
	}
}

func TestCheckSLO_TTFBMapsToStartTransfer(t *testing.T) {
	slo := &SLO{Keys: []string{"ttfb"}, Vals: map[string]int{"ttfb": 50}}
	r := checkSLO(slo, makeTimings(nil))
	if r.Pass || r.Violations[0].ActualMs != 80 {
		t.Fatalf("got %+v", r)
	}
}

func TestCheckSLO_TLSMapsToPretransfer(t *testing.T) {
	slo := &SLO{Keys: []string{"tls"}, Vals: map[string]int{"tls": 20}}
	r := checkSLO(slo, makeTimings(nil))
	if r.Pass || r.Violations[0].ActualMs != 30 {
		t.Fatalf("got %+v", r)
	}
}

func makeCurlData() *CurlData {
	return &CurlData{
		Times: map[string]int{
			"time_namelookup":    5,
			"time_connect":       15,
			"time_pretransfer":   30,
			"time_starttransfer": 80,
			"time_total":         100,
		},
		SpeedDownload:   10240,
		SpeedUpload:     5120,
		RemoteIP:        "1.2.3.4",
		RemotePort:      "443",
		LocalIP:         "10.0.0.1",
		LocalPort:       "50000",
		RangeDNS:        5,
		RangeConnection: 10,
		RangeSSL:        15,
		RangeServer:     50,
		RangeTransfer:   20,
	}
}

func TestBuildJSONResult_SchemaVersion(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200\r\ncontent-type: text/html", nil, 0)
	if r.SchemaVersion != 1 {
		t.Fatalf("schema_version=%d, want 1", r.SchemaVersion)
	}
}

func TestBuildJSONResult_BasicFields(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", nil, 0)
	if r.URL != "https://x" || !r.OK || r.ExitCode != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestBuildJSONResult_ResponseFields(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200\r\ncontent-type: text/html\r\nX-Foo: bar", nil, 0)
	if r.Response.StatusCode != 200 {
		t.Fatalf("status_code=%d, want 200", r.Response.StatusCode)
	}
	if r.Response.StatusLine != "HTTP/2 200" {
		t.Fatalf("status_line=%q, want HTTP/2 200", r.Response.StatusLine)
	}
	if r.Response.RemoteIP != "1.2.3.4" || r.Response.RemotePort != "443" {
		t.Fatalf("got %+v", r.Response)
	}
	if r.Response.Headers["content-type"] != "text/html" || r.Response.Headers["X-Foo"] != "bar" {
		t.Fatalf("headers=%v", r.Response.Headers)
	}
}

func TestBuildJSONResult_TimingsMs(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", nil, 0)
	tm := r.TimingsMs
	if tm.DNS != 5 || tm.Connect != 10 || tm.TLS != 15 || tm.Server != 50 || tm.Transfer != 20 {
		t.Fatalf("ranges: %+v", tm)
	}
	if tm.Total != 100 || tm.Namelookup != 5 || tm.InitialConnect != 15 || tm.Pretransfer != 30 || tm.Starttransfer != 80 {
		t.Fatalf("absolutes: %+v", tm)
	}
}

func TestSpeed(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", nil, 0)
	if r.Speed.DownloadKbs != FloatOneDecimal(10.0) || r.Speed.UploadKbs != FloatOneDecimal(5.0) {
		t.Fatalf("speed=%+v", r.Speed)
	}
}

func TestBuildJSONResult_SLONone(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", nil, 0)
	if r.SLO != nil {
		t.Fatalf("SLO=%v, want nil", r.SLO)
	}
}

func TestBuildJSONResult_SLOPass(t *testing.T) {
	slo := &SLOResult{Pass: true, Violations: []Violation{}}
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", slo, 0)
	if r.SLO == nil || !r.SLO.Pass {
		t.Fatalf("got %+v", r.SLO)
	}
}

func TestBuildJSONResult_SLOFail(t *testing.T) {
	slo := &SLOResult{Pass: false, Violations: []Violation{{Key: "total", ThresholdMs: 50, ActualMs: 100}}}
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", slo, 4)
	if r.SLO == nil || r.SLO.Pass || len(r.SLO.Violations) != 1 || r.ExitCode != 4 || r.OK {
		t.Fatalf("got %+v", r)
	}
}

func TestBuildJSONResult_HTTP1StatusLine(t *testing.T) {
	r := buildJSONResult("http://x", makeCurlData(), "HTTP/1.1 404 Not Found\r\n", nil, 0)
	if r.Response.StatusCode != 404 {
		t.Fatalf("status_code=%d, want 404", r.Response.StatusCode)
	}
	if r.Response.StatusLine != "HTTP/1.1 404 Not Found" {
		t.Fatalf("status_line=%q", r.Response.StatusLine)
	}
}

func TestBuildJSONResult_JSONSerializable(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", nil, 0)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var back map[string]interface{}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("round-trip decode error: %v", err)
	}
	if v, _ := back["schema_version"].(float64); v != 1 {
		t.Fatalf("schema_version=%v", back["schema_version"])
	}
	if v, _ := back["url"].(string); v != "https://x" {
		t.Fatalf("url=%v", back["url"])
	}
}

func ansiReset() string { return "\x1b" + "[0m" }
func ansiPrefix(code string) string { return "\x1b" + "[" + code + "m" }

func TestNoColor_MakeColorReturnsPlainWhenNotIsatty(t *testing.T) {
	saved := ISATTY
	defer func() { ISATTY = saved }()
	ISATTY = false
	got := makeColor("31")("hello")
	if got != "hello" {
		t.Fatalf("got %q, want plain 'hello' when ISATTY=false", got)
	}
}

func TestNoColor_MakeColorReturnsColoredWhenIsatty(t *testing.T) {
	saved := ISATTY
	defer func() { ISATTY = saved }()
	ISATTY = true
	got := makeColor("31")("hello")
	want := ansiPrefix("31") + "hello" + ansiReset()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNoColor_IsattyFalseWhenNoColorSet(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if detectTTY() {
		t.Fatal("detectTTY() should return false when NO_COLOR is set")
	}
}

func TestFloatOneDecimal_JSONFormat(t *testing.T) {
	b, err := json.Marshal(FloatOneDecimal(10.0))
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(b) != "10.0" {
		t.Fatalf("got %q, want \"10.0\"", string(b))
	}
}

func TestJSONKeyOrder(t *testing.T) {
	r := buildJSONResult("https://x", makeCurlData(), "HTTP/2 200", nil, 0)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	s := string(b)
	idxSchema := indexInJSON(s, `"schema_version"`)
	idxURL := indexInJSON(s, `"url"`)
	idxOK := indexInJSON(s, `"ok"`)
	idxExit := indexInJSON(s, `"exit_code"`)
	idxResp := indexInJSON(s, `"response"`)
	idxTim := indexInJSON(s, `"timings_ms"`)
	idxSpeed := indexInJSON(s, `"speed"`)
	idxSLO := indexInJSON(s, `"slo"`)
	if !(idxSchema < idxURL && idxURL < idxOK && idxOK < idxExit && idxExit < idxResp && idxResp < idxTim && idxTim < idxSpeed && idxSpeed < idxSLO) {
		t.Fatalf("key order wrong: %s", s)
	}
}

func indexInJSON(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestEnv_GetReturnsDefaultWhenUnset(t *testing.T) {
	e := &Env{Key: "HTTPSTAT_TEST_UNSET_VARIABLE_XYZ"}
	os.Unsetenv(e.Key)
	got := e.Get("fallback")
	if got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestEnv_GetReturnsValueWhenSet(t *testing.T) {
	e := &Env{Key: "HTTPSTAT_TEST_SET_VARIABLE_XYZ"}
	t.Setenv(e.Key, "actual")
	got := e.Get("fallback")
	if got != "actual" {
		t.Fatalf("got %q, want actual", got)
	}
}

func TestEnv_GetReturnsEmptyStringWhenExplicitlyEmpty(t *testing.T) {
	e := &Env{Key: "HTTPSTAT_TEST_EMPTY_VARIABLE_XYZ"}
	t.Setenv(e.Key, "")
	got := e.Get("fallback")
	if got != "" {
		t.Fatalf("got %q, want empty (env set to empty is distinct from unset)", got)
	}
}

func TestEnv_RegistryPreservesDeclarationOrder(t *testing.T) {
	want := []string{
		"HTTPSTAT_SHOW_BODY",
		"HTTPSTAT_SHOW_IP",
		"HTTPSTAT_SHOW_SPEED",
		"HTTPSTAT_SAVE_BODY",
		"HTTPSTAT_CURL_BIN",
		"HTTPSTAT_METRICS_ONLY",
		"HTTPSTAT_DEBUG",
	}
	if len(envInstances) < len(want) {
		t.Fatalf("envInstances=%d entries, want at least %d", len(envInstances), len(want))
	}
	for i, w := range want {
		if envInstances[i].Key != w {
			t.Fatalf("envInstances[%d]=%q, want %q", i, envInstances[i].Key, w)
		}
	}
}

func TestParseCurlOutput_SecondsFloatPrec761(t *testing.T) {
	raw := []byte(`{
"time_namelookup": 0.005,
"time_connect": 0.015,
"time_pretransfer": 0.030,
"time_starttransfer": 0.080,
"time_total": 0.100,
"time_redirect": 0.000,
"time_appconnect": 0.025,
"speed_download": 10240,
"speed_upload": 5120,
"remote_ip": "1.2.3.4",
"remote_port": "443",
"local_ip": "10.0.0.1",
"local_port": "50000"
}`)
	d, err := parseCurlOutput(raw)
	if err != nil {
		t.Fatalf("parseCurlOutput: %v", err)
	}
	if d.Times["time_namelookup"] != 5 {
		t.Fatalf("time_namelookup=%d, want 5", d.Times["time_namelookup"])
	}
	if d.Times["time_total"] != 100 {
		t.Fatalf("time_total=%d, want 100", d.Times["time_total"])
	}
	if d.RemoteIP != "1.2.3.4" || d.RemotePort != "443" {
		t.Fatalf("remote=%s:%s", d.RemoteIP, d.RemotePort)
	}
	if d.RangeConnection != 10 {
		t.Fatalf("range_connection=%d, want 10", d.RangeConnection)
	}
}

func TestParseCurlOutput_MicrosecondsIntPost761(t *testing.T) {
	raw := []byte(`{
"time_namelookup": 5000,
"time_connect": 15000,
"time_pretransfer": 30000,
"time_starttransfer": 80000,
"time_total": 100000,
"speed_download": 10240.5,
"speed_upload": 5120.0,
"remote_ip": "1.2.3.4",
"remote_port": "80",
"local_ip": "10.0.0.1",
"local_port": "50000"
}`)
	d, err := parseCurlOutput(raw)
	if err != nil {
		t.Fatalf("parseCurlOutput: %v", err)
	}
	if d.Times["time_namelookup"] != 5 {
		t.Fatalf("time_namelookup=%d, want 5", d.Times["time_namelookup"])
	}
	if d.Times["time_total"] != 100 {
		t.Fatalf("time_total=%d, want 100", d.Times["time_total"])
	}
}

func TestParseCurlOutput_InvalidJSONReturnsError(t *testing.T) {
	if _, err := parseCurlOutput([]byte("not json")); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCenterString_EvenPadding(t *testing.T) {
	got := centerString("ab", 6)
	if got != "  ab  " {
		t.Fatalf("got %q, want %q", got, "  ab  ")
	}
}

func TestCenterString_OddPaddingExtraRight(t *testing.T) {
	got := centerString("abc", 6)
	if got != " abc  " {
		t.Fatalf("got %q, want %q (extra space goes right)", got, " abc  ")
	}
}

func TestCenterString_WidthNotLargerThanString(t *testing.T) {
	got := centerString("abcdefgh", 4)
	if got != "abcdefgh" {
		t.Fatalf("got %q, want string unchanged when width <= len", got)
	}
}

func TestLeftAlign_PadsRight(t *testing.T) {
	got := leftAlign("abc", 7)
	if got != "abc    " {
		t.Fatalf("got %q, want %q", got, "abc    ")
	}
}

func TestLeftAlign_WidthNotLargerThanString(t *testing.T) {
	got := leftAlign("abcdefg", 3)
	if got != "abcdefg" {
		t.Fatalf("got %q, want string unchanged when width <= len", got)
	}
}

func TestFmta_CentersMsWithSuffix(t *testing.T) {
	saved := ISATTY
	defer func() { ISATTY = saved }()
	ISATTY = false
	got := fmta(5)
	if got != "  5ms  " {
		t.Fatalf("got %q, want %q", got, "  5ms  ")
	}
}

func TestFmtb_LeftAlignsMsWithSuffix(t *testing.T) {
	saved := ISATTY
	defer func() { ISATTY = saved }()
	ISATTY = false
	got := fmtb(5)
	if got != "5ms    " {
		t.Fatalf("got %q, want %q", got, "5ms    ")
	}
}

func TestDebugLogger_DisabledEmitsNothing(t *testing.T) {
	out := captureStderr(t, func() {
		d := &debugLogger{enabled: false}
		d.log("silent %s", "message")
	})
	if out != "" {
		t.Fatalf("expected no output when disabled, got %q", out)
	}
}

func TestDebugLogger_EnabledWritesToStderr(t *testing.T) {
	out := captureStderr(t, func() {
		d := &debugLogger{enabled: true}
		d.log("hello %s", "world")
	})
	if !strings.Contains(out, "DEBUG:httpstat:hello world") {
		t.Fatalf("stderr=%q, want contains DEBUG:httpstat:hello world", out)
	}
}

func TestPrintHelp_ContainsUsage(t *testing.T) {
	out := captureStdout(t, func() { printHelp() })
	if !strings.Contains(out, "Usage: httpstat") {
		t.Fatalf("printHelp output missing usage line: %q", out)
	}
	if !strings.Contains(out, "HTTPSTAT_SHOW_BODY") {
		t.Fatalf("printHelp output missing env docs: %q", out)
	}
}

func TestReadTrimmed_ReadsAndTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(p, []byte("  hello world\n\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readTrimmed(p)
	if err != nil {
		t.Fatalf("readTrimmed: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestReadTrimmed_MissingFileReturnsError(t *testing.T) {
	_, err := readTrimmed(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error opening missing file, got nil")
	}
}

func TestRun_NoArgsPrintsHelp(t *testing.T) {
	out := captureStdout(t, func() {
		code := run(nil)
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(out, "Usage: httpstat") {
		t.Fatalf("run(nil) stdout missing help: %q", out)
	}
}

func TestRun_HelpFlagLongPrintsHelp(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--help"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(out, "Usage: httpstat") {
		t.Fatalf("stdout missing help: %q", out)
	}
}

func TestRun_HelpFlagShortPrintsHelp(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"-h"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(out, "Usage: httpstat") {
		t.Fatalf("stdout missing help: %q", out)
	}
}

func TestRun_VersionFlagPrintsVersion(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--version"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(out, Version) {
		t.Fatalf("stdout %q missing version %q", out, Version)
	}
}

func TestRun_InvalidFormatRejected(t *testing.T) {
	captureStdout(t, func() {
		code := run([]string{"--format", "xml", "https://example.com"})
		if code != 1 {
			t.Fatalf("exit=%d, want 1", code)
		}
	})
}

func TestRun_InvalidSLOSpecRejected(t *testing.T) {
	captureStdout(t, func() {
		code := run([]string{"--slo", "bogus=100", "https://example.com"})
		if code != 1 {
			t.Fatalf("exit=%d, want 1", code)
		}
	})
}

func TestRun_ExcludedCurlArgRejected(t *testing.T) {
	captureStdout(t, func() {
		code := run([]string{"https://example.com", "-w", "extra"})
		if code != 1 {
			t.Fatalf("exit=%d, want 1", code)
		}
	})
}

func TestRun_InvalidEnvBoolRejected(t *testing.T) {
	t.Setenv("HTTPSTAT_SHOW_BODY", "maybe")
	captureStdout(t, func() {
		code := run([]string{"https://example.com"})
		if code != 1 {
			t.Fatalf("exit=%d, want 1", code)
		}
	})
}

func TestRun_CurlBinNotFoundReturnsNonZero(t *testing.T) {
	t.Setenv("HTTPSTAT_CURL_BIN", filepath.Join(t.TempDir(), "no-such-binary"))
	captureStdout(t, func() {
		code := run([]string{"https://example.com"})
		if code == 0 {
			t.Fatal("expected non-zero exit when curl bin missing")
		}
	})
}

func TestRun_HappyPathJSONFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, defaultFakeHeaders, defaultFakeBody, defaultFakeStdout, 0)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	t.Setenv("HTTPSTAT_SAVE_BODY", "false")
	out := captureStdout(t, func() {
		code := run([]string{"--format", "json", "https://example.com"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v; out=%q", err, out)
	}
	if v, _ := parsed["schema_version"].(float64); v != 1 {
		t.Fatalf("schema_version=%v", parsed["schema_version"])
	}
	if v, _ := parsed["url"].(string); v != "https://example.com" {
		t.Fatalf("url=%v", parsed["url"])
	}
}

func TestRun_HappyPathJSONLFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, defaultFakeHeaders, defaultFakeBody, defaultFakeStdout, 0)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	t.Setenv("HTTPSTAT_SAVE_BODY", "false")
	out := captureStdout(t, func() {
		code := run([]string{"--format", "jsonl", "https://example.com"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	trimmed := strings.TrimSpace(out)
	if strings.Contains(trimmed, "\n") {
		t.Fatalf("jsonl output should be a single line, got %q", trimmed)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v; out=%q", err, trimmed)
	}
}

func TestRun_HappyPathPrettyFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, defaultFakeHeaders, defaultFakeBody, defaultFakeStdout, 0)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	t.Setenv("HTTPSTAT_SHOW_SPEED", "true")
	t.Setenv("HTTPSTAT_SHOW_BODY", "true")
	t.Setenv("HTTPSTAT_SAVE_BODY", "false")
	saved := ISATTY
	defer func() { ISATTY = saved }()
	ISATTY = false
	out := captureStdout(t, func() {
		code := run([]string{"https://example.com"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	if !strings.Contains(out, "Connected to") {
		t.Fatalf("pretty output missing 'Connected to': %q", out)
	}
	if !strings.Contains(out, "DNS Lookup") {
		t.Fatalf("pretty output missing template header: %q", out)
	}
	if !strings.Contains(out, "speed_download") {
		t.Fatalf("pretty output missing speed line: %q", out)
	}
}

func TestRun_MetricsOnlyBackwardCompatSwitchesToJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, defaultFakeHeaders, defaultFakeBody, defaultFakeStdout, 0)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	t.Setenv("HTTPSTAT_METRICS_ONLY", "true")
	t.Setenv("HTTPSTAT_SAVE_BODY", "false")
	out := captureStdout(t, func() {
		code := run([]string{"https://example.com"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("expected JSON output when METRICS_ONLY=true, got %q", out)
	}
}

func TestRun_SLOViolationExitsFour(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, defaultFakeHeaders, defaultFakeBody, defaultFakeStdout, 0)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	t.Setenv("HTTPSTAT_SAVE_BODY", "false")
	captureStdout(t, func() {
		code := run([]string{"--format", "json", "--slo", "total=1", "https://example.com"})
		if code != 4 {
			t.Fatalf("exit=%d, want 4 (SLO violation)", code)
		}
	})
}

func TestRun_SavePathWritesJSONFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, defaultFakeHeaders, defaultFakeBody, defaultFakeStdout, 0)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	t.Setenv("HTTPSTAT_SAVE_BODY", "false")
	savePath := filepath.Join(t.TempDir(), "saved.json")
	captureStdout(t, func() {
		code := run([]string{"--format", "json", "--save", savePath, "https://example.com"})
		if code != 0 {
			t.Fatalf("exit=%d, want 0", code)
		}
	})
	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("read save file: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("save file not JSON: %v; content=%q", err, string(data))
	}
	if v, _ := parsed["url"].(string); v != "https://example.com" {
		t.Fatalf("url in save file=%v", parsed["url"])
	}
}

func TestRun_CurlFailureNonZeroReturned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake curl uses POSIX shell")
	}
	script := writeFakeCurl(t, "", "", "", 22)
	t.Setenv("HTTPSTAT_CURL_BIN", script)
	captureStdout(t, func() {
		code := run([]string{"https://example.com"})
		if code == 0 {
			t.Fatal("expected non-zero exit when curl fails")
		}
	})
}

const defaultFakeHeaders = "HTTP/2 200\r\ncontent-type: text/html\r\nX-Foo: bar\r\n"

const defaultFakeBody = "<html>hi</html>"

const defaultFakeStdout = `{
"time_namelookup": 0.005,
"time_connect": 0.015,
"time_appconnect": 0.025,
"time_pretransfer": 0.030,
"time_redirect": 0.000,
"time_starttransfer": 0.080,
"time_total": 0.100,
"speed_download": 10240,
"speed_upload": 5120,
"remote_ip": "1.2.3.4",
"remote_port": "443",
"local_ip": "10.0.0.1",
"local_port": "50000"
}`

func writeFakeCurl(t *testing.T, headers, body, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	headersPath := filepath.Join(dir, "headers.txt")
	bodyPath := filepath.Join(dir, "body.txt")
	stdoutPath := filepath.Join(dir, "stdout.txt")
	if err := os.WriteFile(headersPath, []byte(headers), 0644); err != nil {
		t.Fatalf("write headers: %v", err)
	}
	if err := os.WriteFile(bodyPath, []byte(body), 0644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := os.WriteFile(stdoutPath, []byte(stdout), 0644); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	scriptPath := filepath.Join(dir, "fakecurl")
	script := fmt.Sprintf(`#!/bin/sh
HDR=""; OUT=""; NEXT=""
for arg in "$@"; do
    if [ "$NEXT" = "hdr" ]; then HDR="$arg"; NEXT=""; continue; fi
    if [ "$NEXT" = "out" ]; then OUT="$arg"; NEXT=""; continue; fi
    if [ "$NEXT" = "skip" ]; then NEXT=""; continue; fi
    case "$arg" in
        -w) NEXT=skip ;;
        -D) NEXT=hdr ;;
        -o) NEXT=out ;;
    esac
done
if [ -n "$HDR" ]; then cp %q "$HDR"; fi
if [ -n "$OUT" ]; then cp %q "$OUT"; fi
cat %q
exit %d
`, headersPath, bodyPath, stdoutPath, exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fakecurl: %v", err)
	}
	return scriptPath
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return string(<-done)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	fn()
	w.Close()
	os.Stderr = orig
	return string(<-done)
}
