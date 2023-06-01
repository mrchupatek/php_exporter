package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type PHPStatusData struct {
	Pool               string    `json:"pool"`
	ProcessManager     string    `json:"process manager"`
	StartTime          int       `json:"start time"`
	StartSince         int       `json:"start since"`
	AcceptedConn       int       `json:"accepted conn"`
	ListenQueue        int       `json:"listen queue"`
	MaxListenQueue     int       `json:"max listen queue"`
	ListenQueueLen     int       `json:"listen queue len"`
	IdleProcesses      int       `json:"idle processes"`
	ActiveProcesses    int       `json:"active processes"`
	TotalProcesses     int       `json:"total processes"`
	MaxActiveProcesses int       `json:"max active processes"`
	MaxChildrenReached int       `json:"max children reached"`
	SlowRequests       int       `json:"slow requests"`
	Processes          []Process `json:"processes"`
}
type Process struct {
	Pid               int     `json:"pid"`
	State             string  `json:"state"`
	StartTime         int     `json:"start time"`
	StartSince        int     `json:"start since"`
	Requests          int     `json:"requests"`
	RequestDuration   int     `json:"request duration"`
	RequestMethod     string  `json:"request method"`
	RequestURI        string  `json:"request uri"`
	ContentLength     int     `json:"content length"`
	User              string  `json:"user"`
	Script            string  `json:"script"`
	LastRequestCPU    float64 `json:"last request cpu"`
	LastRequestMemory int     `json:"last request memory"`
}

type ConfigData struct {
	LoginBA      string `toml:"login_ba"`
	PassBA       string `toml:"password_ba"`
	Base64Loki   string `toml:"base64_loki"`
	LoginPushGW  string `toml:"login_pushgw"`
	PassPushGW   string `toml:"password_pushgw"`
	UrlPHPStatus string `toml:"url_php_status"`
	UrlLoki      string `toml:"url_loki"`
	UrlPushGW    string `toml:"url_pushgw"`
	JobName      string `toml:"job_name"`
}

type Streames struct {
	Stream struct {
		Job          string `json:"job"`
		UrlPHPStatus string `json:"url_php_status"`
	} `json:"stream"`
	Values [][]string `json:"values"`
}

var (
	configPath string
)

func init() {
	flag.StringVar(&configPath, "config-path", "config.toml", "path to config file")

}

func main() {
	flag.Parse()
	config := NewConfigData()
	_, err := toml.DecodeFile(configPath, config)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("=== Start Exporter ===")

	phpData, err := GetPHPStatus(config.UrlPHPStatus)
	if err != nil {
		log.Fatal(err)
	}
	ExamplePusher_Push(config, "php_status", phpData, config.JobName)
	fmt.Println("Delivered")
	resp := AddValueDataLoki(phpData, config.JobName)
	respJson, err := json.Marshal(resp)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(respJson))

	err = PushDataToLoki(config, string(respJson))
	if err != nil {
		log.Fatal(err)
	}

}

//NewConfigData ...
func NewConfigData() *ConfigData {
	return &ConfigData{}
}

func PushDataToLoki(config *ConfigData, data string) error {
	method := "POST"
	payload := strings.NewReader(`{
  "streams": [
		` + data + `
  ]
}`)
	client := &http.Client{}
	req, err := http.NewRequest(method, config.UrlLoki, payload)

	if err != nil {
		fmt.Println(err)
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Basic "+config.Base64Loki)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println(string(body))
	return nil
}

func AddValueDataLoki(phpData *PHPStatusData, url string) *Streames {

	values := make([][]string, 0, 2)
	for _, process := range phpData.Processes {
		valuesRow := make([]string, 0, 2)
		t := time.Now().UnixNano()

		valuesRow = append(valuesRow, strconv.Itoa(int(t)))
		valuesRow = append(valuesRow, OneString(process))

		values = append(values, valuesRow)
	}
	data := Streames{}
	data.Values = values

	data.Stream.Job = "php_status"
	data.Stream.UrlPHPStatus = url

	return &data
}

func OneString(p Process) string {
	res := "pid=" + strconv.Itoa(p.Pid)
	res += " state=" + p.State
	res += " start_time=" + strconv.Itoa(p.StartTime)
	res += " start_since=" + strconv.Itoa(p.StartSince)
	res += " requests=" + strconv.Itoa(p.Requests)
	res += " request_duration=" + strconv.Itoa(p.RequestDuration)
	res += " request_method=" + p.RequestMethod
	res += " request_uri=" + p.RequestURI
	res += " content_length=" + strconv.Itoa(p.ContentLength)
	res += " user=" + p.User
	res += " script=" + p.Script
	s := fmt.Sprintf("%.2f", p.LastRequestCPU)
	res += " last_request_cpu=" + s
	res += " last_request_memory=" + strconv.Itoa(p.LastRequestMemory)

	return res
}

func GetPHPStatus(urlStatus string) (*PHPStatusData, error) {
	var phpData *PHPStatusData
	method := "GET"
	client := &http.Client{}
	req, err := http.NewRequest(method, urlStatus, nil)

	if err != nil {
		return nil, err
	}
	//req.Header.Add("Authorization", "Basic YWRtaW46MTIzMzQ=")

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(body, &phpData)
	if err != nil {
		return nil, err
	}

	return phpData, nil
}

func ExamplePusher_Push(config *ConfigData, jobName string, phpData *PHPStatusData, instans string) {
	StartTime := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_start_time",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	StartTime.Add(float64(phpData.StartTime))

	StartSince := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_start_since",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	StartSince.Add(float64(phpData.StartSince))

	AcceptedConn := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_accepted_conn",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	AcceptedConn.Add(float64(phpData.AcceptedConn))

	ListenQueue := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_listen_queue",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	ListenQueue.Add(float64(phpData.ListenQueue))

	MaxListenQueue := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_max_listen_queue",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	MaxListenQueue.Add(float64(phpData.MaxListenQueue))

	ListenQueueLen := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_listen_queue_len",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	ListenQueueLen.Add(float64(phpData.ListenQueueLen))

	IdleProcesses := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_idle_processes",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	IdleProcesses.Add(float64(phpData.IdleProcesses))

	ActiveProcesses := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_active_processes",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	ActiveProcesses.Add(float64(phpData.ActiveProcesses))

	TotalProcesses := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_total_processes",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	TotalProcesses.Add(float64(phpData.TotalProcesses))

	MaxActiveProcesses := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_max_active_processes",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	MaxActiveProcesses.Add(float64(phpData.MaxActiveProcesses))

	MaxChildrenReached := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_max_children_reached",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	MaxChildrenReached.Add(float64(phpData.MaxChildrenReached))

	SlowRequests := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "php_status_slow_requests",
		Help: "The timestamp of the last successful completion of a DB backup.",
	})
	SlowRequests.Add(float64(phpData.SlowRequests))

	if err := push.New(config.UrlPushGW, jobName).
		Collector(StartTime).
		Collector(StartSince).
		Collector(AcceptedConn).
		Collector(ListenQueue).
		Collector(MaxListenQueue).
		Collector(ListenQueueLen).
		Collector(IdleProcesses).
		Collector(ActiveProcesses).
		Collector(TotalProcesses).
		Collector(MaxActiveProcesses).
		Collector(MaxChildrenReached).
		Collector(SlowRequests).
		Grouping("url", instans).
		Grouping("pool", phpData.Pool).
		Grouping("process_manager", phpData.ProcessManager).
		BasicAuth(config.LoginPushGW, config.PassPushGW).
		Push(); err != nil {
		fmt.Println("Could not push completion time to Pushgateway:", err)
	}
}
