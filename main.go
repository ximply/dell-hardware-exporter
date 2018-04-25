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
	listenAddr = kingpin.Arg("unix-sock", "Exporter listen addr. Default is /dev/shm/dellhardware_exporter.sock").
		String()
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
	cmdStr := fmt.Sprintf("`omreport chassis | grep -v Health | grep -v Chassis | grep -v SEVERITY | grep -v For | grep -v \"Hardware Log\" | grep -v Voltages | grep -v Batteries | grep -v Intrusion | sed /^$/d > %s`", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
}

func dellHardwareStoragePDisk() string {
    tmp := "/dev/shm/dellhwspd.tmp"
	cmdStr := fmt.Sprintf("awk -v hardware_physics_disk_number=`omreport storage pdisk controller=0 | grep -c \"^ID\"` -v hardware_physics_disk=`omreport storage pdisk controller=0 | awk '/^Status/{if($NF==\"Ok\") count+=1}END{print count}'` 'BEGIN{if(hardware_physics_disk_number==hardware_physics_disk) {print 1} else {print 0}}' | sed /^$/d > %s", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
}

func dellHardwareStorageVDisk() string {
	tmp := "/dev/shm/dellhwsvd.tmp"
	cmdStr := fmt.Sprintf("awk -v hardware_virtual_disk_number=`omreport storage vdisk controller=0 | grep -c \"^ID\"` -v hardware_virtual_disk=`omreport storage vdisk controller=0 | awk '/^Status/{if($NF==\"Ok\") count+=1}END{print count}'` 'BEGIN{if(hardware_virtual_disk_number==hardware_virtual_disk) {print 1} else {print 0}}' | sed /^$/d > %s", tmp)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Start()
	cmd.Run()
	cmd.Wait()

	str, _ := readFile(tmp)
	return str
}

func dellHardwareNic() string {
	tmp := "/dev/shm/dellhwnic.tmp"
	cmdStr := fmt.Sprintf("awk -v hardware_nic_number=`omreport chassis nics | grep -c \"Interface Name\"` -v hardware_nic=`omreport chassis nics | awk '/^Connection Status/{print $NF}'| wc -l` 'BEGIN{if(hardware_nic_number==hardware_nic) {print 1} else {print 0}}' | sed /^$/d > %s", tmp)
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
    		s2 = strings.TrimLeft(s2, " ")
    		s2 = strings.Replace(s2, " ", "_", 0)
    		s2 = strings.ToLower(s2)
    		if strings.HasPrefix(s1, "OK") {
    			cache.GetInstance().Add(s2, 10 * time.Minute, 1)
			} else {
				cache.GetInstance().Add(s2, 10 * time.Minute, 0)
			}
		}
	}

	spd := dellHardwareStoragePDisk()
	if strings.HasSuffix(spd, "1") {
		cache.GetInstance().Add("physics_disk", 10 * time.Minute, 1)
	} else {
		cache.GetInstance().Add("physics_disk", 10 * time.Minute, 0)
	}

	svd := dellHardwareStorageVDisk()
	if strings.HasSuffix(svd, "1") {
		cache.GetInstance().Add("virtual_disk", 10 * time.Minute, 1)
	} else {
		cache.GetInstance().Add("virtual_disk", 10 * time.Minute, 0)
	}

	nic := dellHardwareNic()
	if strings.HasSuffix(nic, "1") {
		cache.GetInstance().Add("nic", 10 * time.Minute, 1)
	} else {
		cache.GetInstance().Add("nic", 10 * time.Minute, 0)
	}

}

func metrics(w http.ResponseWriter, req *http.Request) {
	ret := ""
    namespace := "dell_hw"

    r, fans := cache.GetInstance().Value("fans")
    fansV, _ := strconv.ParseFloat(fans.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"fans\"} %g\n", namespace, fansV)
	} else {
		ret += fmt.Sprintf("%s{type=\"fans\"} %g\n", namespace, 1)
	}

	r, memory := cache.GetInstance().Value("memory")
	memoryV, _ := strconv.ParseFloat(memory.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"memory\"} %g\n", namespace, memoryV)
	} else {
		ret += fmt.Sprintf("%s{type=\"memory\"} %g\n", namespace, 1)
	}

	r, power_supplies := cache.GetInstance().Value("power_supplies")
	power_suppliesV, _ := strconv.ParseFloat(power_supplies.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"power_supplies\"} %g\n", namespace, power_suppliesV)
	} else {
		ret += fmt.Sprintf("%s{type=\"power_supplies\"} %g\n", namespace, 1)
	}

	r, power_management := cache.GetInstance().Value("power_management")
	power_managementV, _ := strconv.ParseFloat(power_management.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"power_management\"} %g\n", namespace, power_managementV)
	} else {
		ret += fmt.Sprintf("%s{type=\"power_management\"} %g\n", namespace, 1)
	}

	r, processors := cache.GetInstance().Value("processors")
	processorsV, _ := strconv.ParseFloat(processors.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"processors\"} %g\n", namespace, processorsV)
	} else {
		ret += fmt.Sprintf("%s{type=\"processors\"} %g\n", namespace, 1)
	}

	r, temperatures := cache.GetInstance().Value("temperatures")
	temperaturesV, _ := strconv.ParseFloat(temperatures.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"temperatures\"} %g\n", namespace, temperaturesV)
	} else {
		ret += fmt.Sprintf("%s{type=\"temperatures\"} %g\n", namespace, 1)
	}


	r, physics_disk := cache.GetInstance().Value("physics_disk")
	physics_diskV, _ := strconv.ParseFloat(physics_disk.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"physics_disk\"} %g\n", namespace, physics_diskV)
	} else {
		ret += fmt.Sprintf("%s{type=\"physics_disk\"} %g\n", namespace, 1)
	}



	r, virtual_disk := cache.GetInstance().Value("virtual_disk")
	virtual_diskV, _ := strconv.ParseFloat(virtual_disk.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"virtual_disk\"} %g\n", namespace, virtual_diskV)
	} else {
		ret += fmt.Sprintf("%s{type=\"virtual_disk\"} %g\n", namespace, 1)
	}



	r, nic := cache.GetInstance().Value("nic")
	nicV, _ := strconv.ParseFloat(nic.Text, 64)
	if r {
		ret += fmt.Sprintf("%s{type=\"nic\"} %g\n", namespace, nicV)
	} else {
		ret += fmt.Sprintf("%s{type=\"nic\"} %g\n", namespace, 1)
	}

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
