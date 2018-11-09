package main

import (
	"io"
	"net/http"
	"gopkg.in/alecthomas/kingpin.v2"
	"github.com/robfig/cron"
	"os"
	"net"
	"fmt"
	"os/exec"
	"strings"
	"strconv"
	"sync"
)

var (
	serverIp = kingpin.Arg("sip", "Server IP addr").Required().String()
	processorCount = kingpin.Arg("psrcnt", "Processor count").Required().String()
	processorCores = kingpin.Arg("psrcores", "Cores every processor").Required().String()
	processorBrand = kingpin.Arg("psrbd", "Processor brand").Required().String()
	processorSpeed = kingpin.Arg("psrspd", "Processor speed").Required().String()
	totalMemory = kingpin.Arg("tm", "Total memory in MB").Required().String()
	disks = kingpin.Arg("disks", "physics disks").Required().String()
	megacli = kingpin.Arg("megacli", "cmd path").Required().String()
	listenAddr = kingpin.Arg("unix-sock", "Exporter listen addr.").Required().String()
)

var lock sync.RWMutex
var cacheMap map[string]int

func execCmd(cmdStr string) string {
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Wait()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func dellHardwareSummary() string {
	cmdStr := fmt.Sprintf("`omreport chassis | grep -v Health | grep -v Chassis | grep -v SEVERITY | grep -v For | grep -v Hardware | grep -v Voltages | grep -v Batteries | grep -v Intrusion | sed /^$/d`")
	return execCmd(cmdStr)
}

func dellHardwareStoragePDisk() string {
	if useOmreport {
		cmdStr := fmt.Sprintf("awk -v hardware_physics_disk_number=`omreport storage pdisk controller=0 | grep -c ^ID` -v hardware_physics_disk=`omreport storage pdisk controller=0 | awk '/^Status/{if(length($NF)==2) count+=1}END{print count}'` 'BEGIN{if(hardware_physics_disk_number==hardware_physics_disk) {print 1} else {print 0}}' | sed /^$/d")
		return execCmd(cmdStr)
	}

	if len(disksMap) == 0 {
		return "0"
	}

	for k, v := range disksMap {
		for i := 0; i < v; i++ {
			r := execCmd(fmt.Sprintf("smartctl -H -d megaraid,%d %s | grep Status | grep OK | wc -l", i, k))
			if strings.HasPrefix(r, "0") {
				return "0"
			}
		}
	}

	return "1"
}

func dellHardwareStorageVDisk() string {
	if useOmreport {
		cmdStr := fmt.Sprintf("awk -v hardware_virtual_disk_number=`omreport storage vdisk controller=0 | grep -c ^ID` -v hardware_virtual_disk=`omreport storage vdisk controller=0 | awk '/^Status/{if(length($NF)==2) count+=1}END{print count}'` 'BEGIN{if(hardware_virtual_disk_number==hardware_virtual_disk) {print 1} else {print 0}}' | sed /^$/d")
		return execCmd(cmdStr)
	}

	r := execCmd(fmt.Sprintf("%s -PDList -aALL | grep Error", *megacli))
	r = strings.TrimRight(r, "\n")
	l := strings.Split(r, "\n")
	for _, i := range l {
		if !strings.Contains(i, "Count: 0") {
			return "0"
		}
	}
	return "1"
}

func dellHardwareNic() string {
	cmdStr := fmt.Sprintf("awk -v hardware_nic_number=`omreport chassis nics | grep -v Network | grep -v Physical | grep -v Team | grep -v xenbr | grep -v bond |grep -v ovs-system | grep -c Interface` -v hardware_nic=`omreport chassis nics | awk '/^Connection Status/{print $NF}'| wc -l` 'BEGIN{if(hardware_nic_number==hardware_nic) {print 1} else {print 0}}' | sed /^$/d")
	return execCmd(cmdStr)
}

func checkHealth() {
	//Ok       : Fans
	//Ok       : Memory
	//Ok       : Power Supplies
	//Ok       : Power Management
	//Ok       : Processors
	//Ok       : Temperatures
	summary := dellHardwareSummary()
	summary = strings.TrimRight(summary, "\n")
	summaryList := strings.Split(summary, "\n")
	for _, i := range summaryList {
		tmp := strings.Split(i, ":")
		if len(tmp) == 2 {
			// status: OK or Critical
			s1 := tmp[0]
			s1 = strings.TrimSpace(s1)
			// content
			s2 := tmp[1]
			if strings.Contains(s2, "Supplies") {
				s2 = "powersupplies"
			} else if strings.Contains(s2, "Management") {
				s2 = "powermanagement"
			} else if strings.Contains(s2, "Fans") {
				s2 = "fans"
			} else if strings.Contains(s2, "Memory") {
				s2 = "memory"
			} else if strings.Contains(s2, "Processors") {
				s2 = "processors"
			} else if strings.Contains(s2, "Temperatures") {
				s2 = "temperatures"
			}
			if strings.Contains(s1, "Critical") {
				lock.Lock()
				cacheMap[s2] = 0
				lock.Unlock()
			} else {
				lock.Lock()
				cacheMap[s2] = 1
				lock.Unlock()
			}
		}
	}

	spd := dellHardwareStoragePDisk()
	physics_disk_v := 0
	if strings.HasPrefix(spd, "1") {
		physics_disk_v = 1
	}

	svd := dellHardwareStorageVDisk()
	virtual_disk_v := 0
	if strings.HasPrefix(svd, "1") {
		virtual_disk_v = 1
	}

	nic := dellHardwareNic()
	nic_v := 0
	if strings.HasPrefix(nic, "1") {
		nic_v = 1
	}

	lock.Lock()
	cacheMap["physics_disk"] = physics_disk_v
	cacheMap["virtual_disk"] = virtual_disk_v
	cacheMap["nic"] = nic_v
	lock.Unlock()
}

func metrics(w http.ResponseWriter, req *http.Request) {
	ret := ""
	namespace := "dell_hw"

	lock.RLock()
	if v, ok := cacheMap["fans"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"fans\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"fans\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	if v, ok := cacheMap["memory"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"memory\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"memory\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	if v, ok := cacheMap["powersupplies"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_supplies\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_supplies\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	if v, ok := cacheMap["powermanagement"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_management\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_management\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	if v, ok := cacheMap["processors"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"processors\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"processors\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	if v, ok := cacheMap["temperatures"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"temperatures\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"temperatures\"} %g\n",
			namespace, *serverIp, float64(1))
	}


	if v, ok := cacheMap["physics_disk"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"physics_disk\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"physics_disk\"} %g\n",
			namespace, *serverIp, float64(1))
	}


	if v, ok := cacheMap["virtual_disk"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"virtual_disk\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"virtual_disk\"} %g\n",
			namespace, *serverIp, float64(1))
	}


	if v, ok := cacheMap["nic"]; ok {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"nic\"} %g\n",
			namespace, *serverIp, float64(v))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"nic\"} %g\n",
			namespace, *serverIp, float64(1))
	}
	lock.RUnlock()

	psrcnt, _ := strconv.ParseFloat(*processorCount, 64)
	ret += fmt.Sprintf("%s_processors{sip=\"%s\"} %g\n", namespace, *serverIp, psrcnt)
	psrcores, _ := strconv.ParseFloat(*processorCores, 64)
	ret += fmt.Sprintf("%s_processor_cores{sip=\"%s\"} %g\n", namespace, *serverIp, psrcores)
	ret += fmt.Sprintf("%s_processor_brand{sip=\"%s\",brand=\"%s\"} %g\n",
		namespace, *serverIp, *processorBrand, float64(1))
	psrspd, _ := strconv.ParseFloat(*processorSpeed, 64)
	ret += fmt.Sprintf("%s_processor_speed{sip=\"%s\"} %g\n", namespace, *serverIp, psrspd)
	tm, _ := strconv.ParseFloat(*totalMemory, 64)
	ret += fmt.Sprintf("%s_total_memory{sip=\"%s\"} %g\n", namespace, *serverIp, tm)

	io.WriteString(w, ret)
}

var url string
var disksMap map[string]int
var useOmreport bool

func main() {
	kingpin.Version("0.0.1")
	kingpin.Parse()
	addr := ""

	if listenAddr != nil {
		addr = *listenAddr
	} else {
		addr = "/dev/shm/dellhardware_exporter.sock"
	}

	if strings.Contains(execCmd("whereis omreport"), "/") {
		useOmreport = true
	} else {
		useOmreport = false
	}

	if len(*megacli) == 0 {
		panic("error megacli path")
	}

	cacheMap = make(map[string]int)
	disksMap = make(map[string]int)
	// /dev/sda,4;/dev/sdb,4
	if len(*disks) > 0 {
		l := strings.Split(*disks, ";")
		for _, i := range l {
			d := strings.Split(i, ",")
			if len(d) == 2 {
				cnt, err := strconv.Atoi(d[1])
				if err == nil {
					disksMap[d[0]] = cnt
				}
			}
		}
	}

	checkHealth()

	c := cron.New()
	c.AddFunc("0 */5 * * * ?", checkHealth)
	c.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metrics)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Dell Hardware Exporter</title></head>
             <body>
             <h1>Dell Hardware Exporter</h1>
             <p><a href='` + "/metrics" + `'>Metrics</a></p>
             </body>
             </html>`))
	})
	server := http.Server{
		Handler: mux, // http.DefaultServeMux,
	}
	os.Remove(addr)

	listener, err := net.Listen("unix", addr)
	if err != nil {
		panic(err)
	}
	server.Serve(listener)
}