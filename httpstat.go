package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const Version = "2.0.0"

const curlFormat = `{
"time_namelookup": %{time_namelookup},
"time_connect": %{time_connect},
"time_appconnect": %{time_appconnect},
"time_pretransfer": %{time_pretransfer},
"time_redirect": %{time_redirect},
"time_starttransfer": %{time_starttransfer},
"time_total": %{time_total},
"speed_download": %{speed_download},
"speed_upload": %{speed_upload},
"remote_ip": "%{remote_ip}",
"remote_port": "%{remote_port}",
"local_ip": "%{local_ip}",
"local_port": "%{local_port}"
}`

const httpsTemplate = `  DNS Lookup   TCP Connection   TLS Handshake   Server Processing   Content Transfer
[ {a0000}  |     {a0001}    |    {a0002}    |      {a0003}      |      {a0004}     ]
             |                |               |                   |                  |
    namelookup:{b0000}        |               |                   |                  |
                        connect:{b0001}       |                   |                  |
                                    pretransfer:{b0002}           |                  |
                                                      starttransfer:{b0003}          |
                                                                                 total:{b0004}
`

const httpTemplate = `  DNS Lookup   TCP Connection   Server Processing   Content Transfer
[ {a0000}  |     {a0001}    |      {a0003}      |      {a0004}     ]
             |                |                   |                  |
    namelookup:{b0000}        |                   |                  |
                        connect:{b0001}           |                  |
                                      starttransfer:{b0003}          |
                                                                 total:{b0004}
`

type Env struct {
	Key string
}

var envInstances []*Env

func registerEnv(suffix string) *Env {
	e := &Env{Key: "HTTPSTAT_" + suffix}
	envInstances = append(envInstances, e)
	return e
}

func (e *Env) Get(defaultVal string) string {
	if v, ok := os.LookupEnv(e.Key); ok {
		return v
	}
	return defaultVal
}

var (
	envShowBody     = registerEnv("SHOW_BODY")
	envShowIP       = registerEnv("SHOW_IP")
	envShowSpeed    = registerEnv("SHOW_SPEED")
	envSaveBody     = registerEnv("SAVE_BODY")
	envCurlBin      = registerEnv("CURL_BIN")
	envMetricsOnly  = registerEnv("METRICS_ONLY")
	envDebug        = registerEnv("DEBUG")
)

var ISATTY = detectTTY()

func detectTTY() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func makeColor(code string) func(string) string {
	return func(s string) string {
		if ISATTY {
			return "\x1b[" + code + "m" + s + "\x1b[0m"
		}
		return s
	}
}

var (
	red       = makeColor("31")
	green     = makeColor("32")
	yellow    = makeColor("33")
	blue      = makeColor("34")
	magenta   = makeColor("35")
	cyan      = makeColor("36")
	bold      = makeColor("1")
	underline = makeColor("4")
)

var grayscale = func() map[int]func(string) string {
	m := make(map[int]func(string) string, 24)
	for i := 232; i < 256; i++ {
		m[i-232] = makeColor("38;5;" + strconv.Itoa(i))
	}
	return m
}()

var _ = []func(string) string{blue, magenta, bold, underline}

var truthy = map[string]struct{}{"1": {}, "true": {}, "yes": {}, "on": {}}
var falsy = map[string]struct{}{"0": {}, "false": {}, "no": {}, "off": {}}

func parseBool(value string) (bool, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if _, ok := truthy[v]; ok {
		return true, nil
	}
	if _, ok := falsy[v]; ok {
		return false, nil
	}
	return false, fmt.Errorf("not a boolean value: %q", value)
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// popArgValue: matches Python's edge case where a value-taking flag at the
// end of argv (with no value following) is left in place and returns "no value".
func popArgValue(args *[]string, flag string) (string, bool) {
	idx := indexOf(*args, flag)
	if idx < 0 {
		return "", false
	}
	if idx == len(*args)-1 {
		return "", false
	}
	val := (*args)[idx+1]
	*args = append((*args)[:idx], (*args)[idx+2:]...)
	return val, true
}

func popArgFlag(args *[]string, flag string) bool {
	idx := indexOf(*args, flag)
	if idx < 0 {
		return false
	}
	*args = append((*args)[:idx], (*args)[idx+1:]...)
	return true
}

var sloKeyMap = map[string]string{
	"total":   "time_total",
	"connect": "time_connect",
	"ttfb":    "time_starttransfer",
	"dns":     "time_namelookup",
	"tls":     "time_pretransfer",
}

var sloKeyOrder = []string{"total", "connect", "ttfb", "dns", "tls"}

type SLO struct {
	Keys []string
	Vals map[string]int
}

func parseSLO(spec string) (*SLO, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("SLO spec is empty")
	}
	slo := &SLO{Vals: map[string]int{}}
	for _, pair := range strings.Split(spec, ",") {
		if !strings.Contains(pair, "=") {
			return nil, fmt.Errorf("invalid SLO entry %q (expected key=milliseconds)", strings.TrimSpace(pair))
		}
		i := strings.Index(pair, "=")
		key := strings.TrimSpace(pair[:i])
		val := strings.TrimSpace(pair[i+1:])
		if _, ok := sloKeyMap[key]; !ok {
			return nil, fmt.Errorf("unknown SLO key %q (allowed: total, connect, ttfb, dns, tls)", key)
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("SLO value for %q must be an integer, got %q", key, val)
		}
		if n <= 0 {
			return nil, fmt.Errorf("SLO value for %q must be positive, got %d", key, n)
		}
		if _, dup := slo.Vals[key]; !dup {
			slo.Keys = append(slo.Keys, key)
		}
		slo.Vals[key] = n
	}
	return slo, nil
}

type Violation struct {
	Key         string `json:"key"`
	ThresholdMs int    `json:"threshold_ms"`
	ActualMs    int    `json:"actual_ms"`
}

type SLOResult struct {
	Pass       bool        `json:"pass"`
	Violations []Violation `json:"violations"`
}

func checkSLO(slo *SLO, timings map[string]int) *SLOResult {
	if slo == nil {
		return nil
	}
	res := &SLOResult{Pass: true, Violations: []Violation{}}
	for _, k := range slo.Keys {
		threshold := slo.Vals[k]
		actual := timings[sloKeyMap[k]]
		if actual > threshold {
			res.Pass = false
			res.Violations = append(res.Violations, Violation{
				Key:         k,
				ThresholdMs: threshold,
				ActualMs:    actual,
			})
		}
	}
	return res
}

// FloatOneDecimal serializes with a fixed single decimal so 10 → "10.0"
// (Python's round(x, 1) with json.dumps prints the trailing zero).
type FloatOneDecimal float64

func (f FloatOneDecimal) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(f), 'f', 1, 64)), nil
}

func roundOneDecimal(v float64) FloatOneDecimal {
	return FloatOneDecimal(math.Round(v*10) / 10)
}

type Response struct {
	StatusLine string            `json:"status_line"`
	StatusCode int               `json:"status_code"`
	RemoteIP   string            `json:"remote_ip"`
	RemotePort string            `json:"remote_port"`
	Headers    map[string]string `json:"headers"`
}

type TimingsMs struct {
	DNS            int `json:"dns"`
	Connect        int `json:"connect"`
	TLS            int `json:"tls"`
	Server         int `json:"server"`
	Transfer       int `json:"transfer"`
	Total          int `json:"total"`
	Namelookup     int `json:"namelookup"`
	InitialConnect int `json:"initial_connect"`
	Pretransfer    int `json:"pretransfer"`
	Starttransfer  int `json:"starttransfer"`
}

type Speed struct {
	DownloadKbs FloatOneDecimal `json:"download_kbs"`
	UploadKbs   FloatOneDecimal `json:"upload_kbs"`
}

type JSONResult struct {
	SchemaVersion int        `json:"schema_version"`
	URL           string     `json:"url"`
	OK            bool       `json:"ok"`
	ExitCode      int        `json:"exit_code"`
	Response      Response   `json:"response"`
	TimingsMs     TimingsMs  `json:"timings_ms"`
	Speed         Speed      `json:"speed"`
	SLO           *SLOResult `json:"slo"`
}

type CurlData struct {
	Times           map[string]int
	SpeedDownload   float64
	SpeedUpload     float64
	RemoteIP        string
	RemotePort      string
	LocalIP         string
	LocalPort       string
	RangeDNS        int
	RangeConnection int
	RangeSSL        int
	RangeServer     int
	RangeTransfer   int
}

// parseCurlOutput handles both curl <7.61 (seconds as float) and >=7.61
// (microseconds as int) by inspecting whether the raw json.Number contains a dot.
func parseCurlOutput(raw []byte) (*CurlData, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]interface{}
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	cd := &CurlData{Times: map[string]int{}}
	for k, v := range m {
		if strings.HasPrefix(k, "time_") {
			num, ok := v.(json.Number)
			if !ok {
				return nil, fmt.Errorf("time field %s not a number", k)
			}
			s := num.String()
			if strings.Contains(s, ".") {
				f, err := num.Float64()
				if err != nil {
					return nil, err
				}
				cd.Times[k] = int(f * 1000)
			} else {
				n, err := num.Int64()
				if err != nil {
					return nil, err
				}
				cd.Times[k] = int(n / 1000)
			}
		}
	}
	if v, ok := m["speed_download"]; ok {
		if num, ok := v.(json.Number); ok {
			f, _ := num.Float64()
			cd.SpeedDownload = f
		}
	}
	if v, ok := m["speed_upload"]; ok {
		if num, ok := v.(json.Number); ok {
			f, _ := num.Float64()
			cd.SpeedUpload = f
		}
	}
	if v, ok := m["remote_ip"].(string); ok {
		cd.RemoteIP = v
	}
	if v, ok := m["remote_port"].(string); ok {
		cd.RemotePort = v
	}
	if v, ok := m["local_ip"].(string); ok {
		cd.LocalIP = v
	}
	if v, ok := m["local_port"].(string); ok {
		cd.LocalPort = v
	}
	cd.RangeDNS = cd.Times["time_namelookup"]
	cd.RangeConnection = cd.Times["time_connect"] - cd.Times["time_namelookup"]
	cd.RangeSSL = cd.Times["time_pretransfer"] - cd.Times["time_connect"]
	cd.RangeServer = cd.Times["time_starttransfer"] - cd.Times["time_pretransfer"]
	cd.RangeTransfer = cd.Times["time_total"] - cd.Times["time_starttransfer"]
	return cd, nil
}

func buildJSONResult(url string, d *CurlData, headersText string, slo *SLOResult, exitCode int) *JSONResult {
	lines := strings.Split(headersText, "\n")
	statusLine := ""
	if len(lines) > 0 {
		statusLine = strings.TrimRight(lines[0], "\r")
	}
	statusCode := 0
	parts := strings.Fields(statusLine)
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			statusCode = n
		}
	}
	headers := map[string]string{}
	for _, line := range lines[1:] {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		headers[k] = v
	}
	return &JSONResult{
		SchemaVersion: 1,
		URL:           url,
		OK:            exitCode == 0,
		ExitCode:      exitCode,
		Response: Response{
			StatusLine: statusLine,
			StatusCode: statusCode,
			RemoteIP:   d.RemoteIP,
			RemotePort: d.RemotePort,
			Headers:    headers,
		},
		TimingsMs: TimingsMs{
			DNS:            d.RangeDNS,
			Connect:        d.RangeConnection,
			TLS:            d.RangeSSL,
			Server:         d.RangeServer,
			Transfer:       d.RangeTransfer,
			Total:          d.Times["time_total"],
			Namelookup:     d.Times["time_namelookup"],
			InitialConnect: d.Times["time_connect"],
			Pretransfer:    d.Times["time_pretransfer"],
			Starttransfer:  d.Times["time_starttransfer"],
		},
		Speed: Speed{
			DownloadKbs: roundOneDecimal(d.SpeedDownload / 1024),
			UploadKbs:   roundOneDecimal(d.SpeedUpload / 1024),
		},
		SLO: slo,
	}
}

func centerString(s string, width int) string {
	if len(s) >= width {
		return s
	}
	total := width - len(s)
	left := total / 2
	right := total - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func leftAlign(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func fmta(v int) string {
	return cyan(centerString(strconv.Itoa(v)+"ms", 7))
}

func fmtb(v int) string {
	return cyan(leftAlign(strconv.Itoa(v)+"ms", 7))
}

type debugLogger struct {
	enabled bool
}

func (d *debugLogger) log(format string, args ...interface{}) {
	if !d.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "DEBUG:httpstat:"+format+"\n", args...)
}

const helpText = `Usage: httpstat URL [CURL_OPTIONS]
       httpstat -h | --help
       httpstat --version

Arguments:
  URL     url to request, could be with or without ` + "`" + `http(s)://` + "`" + ` prefix

Options:
  --format FMT     output format: pretty (default), json, jsonl
  --slo SPEC       fail (exit 4) if timings exceed thresholds (ms).
                   SPEC is a comma-separated list of key=milliseconds pairs.
                   Allowed keys: total, connect, ttfb, dns, tls.
  --save PATH      save structured JSON result to file (works with all formats)
  CURL_OPTIONS     any curl supported options, except for -w -D -o -S -s,
                   which are already used internally.
  -h --help        show this screen.
  --version        show version.

Environments:
  HTTPSTAT_SHOW_BODY    set to ` + "`true`" + ` to show response body,
                        default is ` + "`false`" + `, set to ` + "`true`" + ` to enable.
  HTTPSTAT_SHOW_IP      by default httpstat shows remote and local IP/port address.
                        set to ` + "`false`" + ` to disable this feature. default is ` + "`true`" + `
  HTTPSTAT_SHOW_SPEED   set to ` + "`true`" + ` to show download and upload speed.
                        default is ` + "`false`" + `.
  HTTPSTAT_SAVE_BODY    by default httpstat stores body in a tmp file,
                        set to ` + "`false`" + ` to disable this feature. default is ` + "`true`" + `
  HTTPSTAT_CURL_BIN     indicate the curl bin path to use. default is ` + "`curl`" + `
                        from current shell $PATH.
  HTTPSTAT_METRICS_ONLY set to ` + "`true`" + ` to only output metrics as JSON,
                        useful for scripting. Kept for backward compatibility;
                        prefer ` + "`--format json`" + ` or ` + "`--format jsonl`" + `.
  HTTPSTAT_DEBUG        set to ` + "`true`" + ` to see debugging logs. default is ` + "`false`" + `
`

func printHelp() {
	fmt.Print(helpText)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return 0
	}

	formatSpec, hasFormat := popArgValue(&args, "--format")
	if !hasFormat {
		formatSpec, hasFormat = popArgValue(&args, "-f")
	}
	sloSpec, hasSlo := popArgValue(&args, "--slo")
	savePath, hasSave := popArgValue(&args, "--save")

	showBodyStr := envShowBody.Get("false")
	showIPStr := envShowIP.Get("true")
	showSpeedStr := envShowSpeed.Get("false")
	saveBodyStr := envSaveBody.Get("true")
	curlBin := envCurlBin.Get("curl")
	metricsOnlyStr := envMetricsOnly.Get("false")
	debugStr := envDebug.Get("false")

	showBody, err := parseBool(showBodyStr)
	if err != nil {
		fmt.Println(yellow("HTTPSTAT_SHOW_BODY: " + err.Error()))
		return 1
	}
	showIP, err := parseBool(showIPStr)
	if err != nil {
		fmt.Println(yellow("HTTPSTAT_SHOW_IP: " + err.Error()))
		return 1
	}
	showSpeed, err := parseBool(showSpeedStr)
	if err != nil {
		fmt.Println(yellow("HTTPSTAT_SHOW_SPEED: " + err.Error()))
		return 1
	}
	saveBody, err := parseBool(saveBodyStr)
	if err != nil {
		fmt.Println(yellow("HTTPSTAT_SAVE_BODY: " + err.Error()))
		return 1
	}
	metricsOnly, err := parseBool(metricsOnlyStr)
	if err != nil {
		fmt.Println(yellow("HTTPSTAT_METRICS_ONLY: " + err.Error()))
		return 1
	}
	debugFlag, err := parseBool(debugStr)
	if err != nil {
		fmt.Println(yellow("HTTPSTAT_DEBUG: " + err.Error()))
		return 1
	}

	if !hasFormat || formatSpec == "" {
		formatSpec = "pretty"
	}
	if metricsOnly && formatSpec == "pretty" {
		formatSpec = "json"
	}
	if formatSpec != "pretty" && formatSpec != "json" && formatSpec != "jsonl" {
		fmt.Println(yellow(fmt.Sprintf("Invalid --format %q (allowed: pretty, json, jsonl)", formatSpec)))
		return 1
	}

	var slo *SLO
	if hasSlo {
		s, err := parseSLO(sloSpec)
		if err != nil {
			fmt.Println(yellow(err.Error()))
			return 1
		}
		slo = s
	}

	dbg := &debugLogger{enabled: debugFlag}
	for _, e := range envInstances {
		dbg.log("Env %s: %q", e.Key, e.Get(""))
	}
	dbg.log("flags: format=%s slo=%v save=%s show_body=%v show_ip=%v show_speed=%v save_body=%v curl_bin=%s metrics_only=%v",
		formatSpec, slo, savePath, showBody, showIP, showSpeed, saveBody, curlBin, metricsOnly)

	if len(args) == 0 {
		printHelp()
		return 0
	}
	url := args[0]
	if url == "-h" || url == "--help" {
		printHelp()
		return 0
	}
	if url == "--version" {
		fmt.Println("httpstat " + Version)
		return 0
	}

	curlArgs := args[1:]
	excludeOptions := []string{"-w", "--write-out", "-D", "--dump-header", "-o", "--output", "-s", "--silent"}
	for _, a := range curlArgs {
		if indexOf(excludeOptions, a) >= 0 {
			fmt.Println(yellow(fmt.Sprintf("Error: %s is not allowed in extra curl args", a)))
			return 1
		}
	}

	bodyFile, err := os.CreateTemp("", "httpstat-body-*")
	if err != nil {
		fmt.Println(yellow("Failed to create tempfile: " + err.Error()))
		return 1
	}
	bodyPath := bodyFile.Name()
	bodyFile.Close()

	headerFile, err := os.CreateTemp("", "httpstat-header-*")
	if err != nil {
		os.Remove(bodyPath)
		fmt.Println(yellow("Failed to create tempfile: " + err.Error()))
		return 1
	}
	headerPath := headerFile.Name()
	headerFile.Close()

	defer func() {
		os.Remove(headerPath)
		if !saveBody {
			dbg.log("rm body file %s", bodyPath)
			os.Remove(bodyPath)
		}
	}()

	cmd := []string{curlBin, "-w", curlFormat, "-D", headerPath, "-o", bodyPath, "-s", "-S"}
	cmd = append(cmd, curlArgs...)
	cmd = append(cmd, url)

	dbg.log("cmd: %v", cmd)

	c := exec.Command(cmd[0], cmd[1:]...)
	env := os.Environ()
	filtered := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "LC_ALL=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	filtered = append(filtered, "LC_ALL=C")
	c.Env = filtered

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err = c.Run()
	returnCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			returnCode = exitErr.ExitCode()
		} else {
			returnCode = 1
		}
	}

	if returnCode != 0 {
		// Sanitize display of the command: mask curl_format at [2], tempfile at [4] and [6].
		display := make([]string, len(cmd))
		copy(display, cmd)
		if len(display) > 2 {
			display[2] = "<output-format>"
		}
		if len(display) > 4 {
			display[4] = "<tempfile>"
		}
		if len(display) > 6 {
			display[6] = "<tempfile>"
		}
		fmt.Println("> " + strings.Join(display, " "))
		fmt.Println(yellow("curl error: " + stderr.String()))
		return returnCode
	}

	if stderr.Len() > 0 {
		fmt.Print(grayscale[16](stderr.String()))
	}

	out := stdout.Bytes()
	d, err := parseCurlOutput(out)
	if err != nil {
		fmt.Println(yellow("Could not decode json: " + err.Error()))
		fmt.Printf("curl result: %d %s %s\n", returnCode, grayscale[16](string(out)), grayscale[16](stderr.String()))
		return 1
	}

	headersText, err := readTrimmed(headerPath)
	if err != nil {
		fmt.Println(yellow("Failed to read headers: " + err.Error()))
		return 1
	}

	exitCode := 0
	sloResult := checkSLO(slo, d.Times)
	if sloResult != nil && !sloResult.Pass {
		exitCode = 4
	}

	if formatSpec == "json" || formatSpec == "jsonl" {
		res := buildJSONResult(url, d, headersText, sloResult, exitCode)
		var payload []byte
		if formatSpec == "json" {
			payload, err = json.MarshalIndent(res, "", "  ")
		} else {
			payload, err = json.Marshal(res)
		}
		if err != nil {
			fmt.Println(yellow("Failed to serialize JSON: " + err.Error()))
			return 1
		}
		fmt.Println(string(payload))
		if hasSave && savePath != "" {
			if err := os.WriteFile(savePath, append(payload, '\n'), 0644); err != nil {
				fmt.Println(yellow("Failed to write --save: " + err.Error()))
				return 1
			}
		}
		return exitCode
	}

	if showIP {
		fmt.Printf("Connected to %s:%s from %s:%s\n",
			cyan(d.RemoteIP), cyan(d.RemotePort), d.LocalIP, d.LocalPort)
		fmt.Println()
	}

	headerLines := strings.Split(headersText, "\n")
	for i, line := range headerLines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if i == 0 {
			slashIdx := strings.Index(line, "/")
			if slashIdx >= 0 {
				fmt.Println(green(line[:slashIdx]) + grayscale[14]("/") + cyan(line[slashIdx+1:]))
			} else {
				fmt.Println(line)
			}
		} else {
			pos := strings.Index(line, ":")
			if pos < 0 {
				fmt.Println(line)
				continue
			}
			fmt.Println(grayscale[14](line[:pos+1]) + cyan(line[pos+1:]))
		}
	}
	fmt.Println()

	if showBody {
		bodyBytes, err := os.ReadFile(bodyPath)
		if err == nil {
			body := string(bodyBytes)
			if len(body) > 1024 {
				fmt.Print(body[:1024])
				fmt.Println(cyan("..."))
				msg := fmt.Sprintf("%s is truncated (1024 out of %d)", green("Body"), len(body))
				if saveBody {
					msg += ", stored in: " + bodyPath
				}
				fmt.Println(msg)
			} else {
				fmt.Println(body)
			}
		}
	} else if saveBody {
		fmt.Printf("%s stored in: %s\n", green("Body"), bodyPath)
	}

	var template string
	if strings.HasPrefix(url, "https://") {
		template = httpsTemplate
	} else {
		template = httpTemplate
	}
	tmplLines := strings.SplitN(template, "\n", 2)
	if len(tmplLines) == 2 {
		template = grayscale[16](tmplLines[0]) + "\n" + tmplLines[1]
	}
	template = strings.ReplaceAll(template, "{a0000}", fmta(d.RangeDNS))
	template = strings.ReplaceAll(template, "{a0001}", fmta(d.RangeConnection))
	template = strings.ReplaceAll(template, "{a0002}", fmta(d.RangeSSL))
	template = strings.ReplaceAll(template, "{a0003}", fmta(d.RangeServer))
	template = strings.ReplaceAll(template, "{a0004}", fmta(d.RangeTransfer))
	template = strings.ReplaceAll(template, "{b0000}", fmtb(d.Times["time_namelookup"]))
	template = strings.ReplaceAll(template, "{b0001}", fmtb(d.Times["time_connect"]))
	template = strings.ReplaceAll(template, "{b0002}", fmtb(d.Times["time_pretransfer"]))
	template = strings.ReplaceAll(template, "{b0003}", fmtb(d.Times["time_starttransfer"]))
	template = strings.ReplaceAll(template, "{b0004}", fmtb(d.Times["time_total"]))
	fmt.Print(template)

	if showSpeed {
		fmt.Printf("speed_download: %.1f KiB/s, speed_upload: %.1f KiB/s\n",
			d.SpeedDownload/1024, d.SpeedUpload/1024)
	}

	if sloResult != nil && !sloResult.Pass {
		fmt.Println()
		fmt.Println(red("SLO violations:"))
		for _, v := range sloResult.Violations {
			fmt.Println(red(fmt.Sprintf("  %s: %d ms > %d ms", v.Key, v.ActualMs, v.ThresholdMs)))
		}
	}

	if hasSave && savePath != "" {
		res := buildJSONResult(url, d, headersText, sloResult, exitCode)
		payload, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			fmt.Println(yellow("Failed to serialize JSON: " + err.Error()))
			return 1
		}
		if err := os.WriteFile(savePath, append(payload, '\n'), 0644); err != nil {
			fmt.Println(yellow("Failed to write --save: " + err.Error()))
			return 1
		}
	}

	return exitCode
}

func readTrimmed(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
