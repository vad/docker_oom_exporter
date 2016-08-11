package main

import (
	"flag"
	"fmt"
	docker "github.com/docker/engine-api/client"
	"github.com/hpcloud/tail"
	"golang.org/x/net/context"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"
)

const (
	METRIC_NAME        = "oom_totals"
	METRIC_DESCRIPTION = "OOM counts per container"
)

var (
	oomCounter = make(map[string]int)
	input      = flag.String("input", "/var/log/kern.log", "Input file")
	output     = flag.String("output", "/tmp/cacca.prom", "Output file")
)

func write() {
	log.Println("Write")

	// write oomCounter to tmp file
	tmpfile, err := ioutil.TempFile("", "prom_oom")

	if err != nil {
		panic("Cannot open temporary file")
	}

	fmt.Fprintf(tmpfile, "# HELP %s %s\n", METRIC_NAME, METRIC_DESCRIPTION)
	fmt.Fprintf(tmpfile, "# TYPE %s\n", METRIC_NAME)

	for k := range oomCounter {
		fmt.Fprintf(tmpfile, "%s{container=\"%s\"} %d\n", METRIC_NAME, k, oomCounter[k])
	}
	if err = tmpfile.Close(); err != nil {
		panic("Cannot close temporary file: " + err.Error())
	}

	// atomic mv tmp file to output dir
	if err = os.Rename(tmpfile.Name(), *output); err != nil {
		panic("Cannot write to output file: " + err.Error())
	}
}

func main() {
	flag.Parse()

	t, err := tail.TailFile(*input, tail.Config{Follow: true})
	if err != nil {
		log.Fatalln("Log file does not exist")
	}
	re := regexp.MustCompile(".*cpuset=(.{12})")
	reCName := regexp.MustCompile("[a-zA-Z-]+-[0-9]+-([a-zA-Z-]+)-[a-f0-9]{20}")

	defaultHeaders := map[string]string{"User-Agent": "engine-api-cli-1.0"}
	dc, err := docker.NewClient("unix:///var/run/docker.sock", "v1.21", nil, defaultHeaders)
	if err != nil {
		log.Fatalln("Can't connect to docker socket")
	}

	for line := range t.Lines {
		if strings.Contains(line.Text, "invoked oom-killer") {

			// get next line, it contains the container ID
			l := <-t.Lines

			// container ID
			cid := re.FindStringSubmatch(l.Text)[1]

			container, err := dc.ContainerInspect(context.Background(), cid)

			if err != nil {
				log.Printf("Error getting information about container %s (skipping): %e\n", cid, err.Error())
			} else {
				name := container.Name
				match := reCName.FindStringSubmatch(name)
				if len(match) < 2 {
					log.Println("Container name does not match pattern")
				} else {
					name = match[1]
				}
				oomCounter[name] += 1
			}

			write()
		}
	}
}
