package main

import (
	"io"
	"net/http"
	"gopkg.in/alecthomas/kingpin.v2"
	"github.com/robfig/cron"
	"os"
	"net"
	"io/ioutil"
	"fmt"
	"os/exec"
	"github.com/ximply/dell-hardware-exporter/cache"
	"strings"
	"time"
	"strconv"
)

var (
	serverIp = kingpin.Arg("sip", "Server IP addr").Required().String()
	processorCount = kingpin.Arg("psrcnt", "Processor count").Required().String()
	processorCores = kingpin.Arg("psrcores", "Cores every processor").Required().String()
	processorBrand = kingpin.Arg("psrbd", "Processor brand").Required().String()
	processorSpeed = kingpin.Arg("psrspd", "Processor speed").Required().String()
	totalMemory = kingpin.Arg("tm", "Total memory in MB").Required().String()
	listenAddr = kingpin.Arg("unix-sock", "Exporter listen addr.").Required().String()
)

func readFile(file string) (string, error) {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func dellHardwareSummary() string {
	tmp := "/dev/shm/dellhwsumm.tmp"
	cmdStr := fmt.Sprintf("`omreport chassis | grep -v Health | grep -v Chassis | grep -v SEVERITY | grep -v For | grep -v Hardware | grep -v Voltages | grep -v Batteries | grep -v Intrusion | sed /^$/d > %s`", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
}

func dellHardwareStoragePDisk() string {
	tmp := "/dev/shm/dellhwspd.tmp"
	cmdStr := fmt.Sprintf("awk -v hardware_physics_disk_number=`omreport storage pdisk controller=0 | grep -c ^ID` -v hardware_physics_disk=`omreport storage pdisk controller=0 | awk '/^Status/{if(length($NF)==2) count+=1}END{print count}'` 'BEGIN{if(hardware_physics_disk_number==hardware_physics_disk) {print 1} else {print 0}}' | sed /^$/d > %s", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
}

func dellHardwareStorageVDisk() string {
	tmp := "/dev/shm/dellhwsvd.tmp"
	cmdStr := fmt.Sprintf("awk -v hardware_virtual_disk_number=`omreport storage vdisk controller=0 | grep -c ^ID` -v hardware_virtual_disk=`omreport storage vdisk controller=0 | awk '/^Status/{if(length($NF)==2) count+=1}END{print count}'` 'BEGIN{if(hardware_virtual_disk_number==hardware_virtual_disk) {print 1} else {print 0}}' | sed /^$/d > %s", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
}

func dellHardwareNic() string {
	tmp := "/dev/shm/dellhwnic.tmp"
	cmdStr := fmt.Sprintf("awk -v hardware_nic_number=`omreport chassis nics | grep -v Network | grep -v Physical | grep -v Team | grep -v xenbr | grep -v bond |grep -v ovs-system | grep -c Interface` -v hardware_nic=`omreport chassis nics | awk '/^Connection Status/{print $NF}'| wc -l` 'BEGIN{if(hardware_nic_number==hardware_nic) {print 1} else {print 0}}' | sed /^$/d > %s", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
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
				cache.GetInstance().Add(s2, 10 * time.Minute, 0)
			} else {
				cache.GetInstance().Add(s2, 10 * time.Minute, 1)
			}
		}
	}

	spd := dellHardwareStoragePDisk()
	if strings.HasPrefix(spd, "1") {
		cache.GetInstance().Add("physics_disk", 10 * time.Minute, 1)
	} else {
		cache.GetInstance().Add("physics_disk", 10 * time.Minute, 0)
	}

	svd := dellHardwareStorageVDisk()
	if strings.HasPrefix(svd, "1") {
		cache.GetInstance().Add("virtual_disk", 10 * time.Minute, 1)
	} else {
		cache.GetInstance().Add("virtual_disk", 10 * time.Minute, 0)
	}

	nic := dellHardwareNic()
	if strings.HasPrefix(nic, "1") {
		cache.GetInstance().Add("nic", 10 * time.Minute, 1)
	} else {
		cache.GetInstance().Add("nic", 10 * time.Minute, 0)
	}
}

func metrics(w http.ResponseWriter, req *http.Request) {
	ret := ""
	namespace := "dell_hw"

	r, fans := cache.GetInstance().Value("fans")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"fans\"} %g\n",
			namespace, *serverIp, float64(fans.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"fans\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	r, memory := cache.GetInstance().Value("memory")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"memory\"} %g\n",
			namespace, *serverIp, float64(memory.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"memory\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	r, powersupplies := cache.GetInstance().Value("powersupplies")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_supplies\"} %g\n",
			namespace, *serverIp, float64(powersupplies.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_supplies\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	r, powermanagement := cache.GetInstance().Value("powermanagement")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_management\"} %g\n",
			namespace, *serverIp, float64(powermanagement.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"power_management\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	r, processors := cache.GetInstance().Value("processors")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"processors\"} %g\n",
			namespace, *serverIp, float64(processors.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"processors\"} %g\n",
			namespace, *serverIp, float64(1))
	}

	r, temperatures := cache.GetInstance().Value("temperatures")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"temperatures\"} %g\n",
			namespace, *serverIp, float64(temperatures.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"temperatures\"} %g\n",
			namespace, *serverIp, float64(1))
	}


	r, physics_disk := cache.GetInstance().Value("physics_disk")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"physics_disk\"} %g\n",
			namespace, *serverIp, float64(physics_disk.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"physics_disk\"} %g\n",
			namespace, *serverIp, float64(1))
	}



	r, virtual_disk := cache.GetInstance().Value("virtual_disk")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"virtual_disk\"} %g\n",
			namespace, *serverIp, float64(virtual_disk.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"virtual_disk\"} %g\n",
			namespace, *serverIp, float64(1))
	}



	r, nic := cache.GetInstance().Value("nic")
	if r {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"nic\"} %g\n",
			namespace, *serverIp, float64(nic.(int)))
	} else {
		ret += fmt.Sprintf("%s_health{sip=\"%s\",type=\"nic\"} %g\n",
			namespace, *serverIp, float64(1))
	}

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

func main() {
	kingpin.Version("0.0.1")
	kingpin.Parse()
	addr := ""

	if listenAddr != nil {
		addr = *listenAddr
	} else {
		addr = "/dev/shm/dellhardware_exporter.sock"
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