package main

import (
	"bytes"
	"encoding/json"
	_ "expvar"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	syslog "gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"
)

var token string

type Event struct {
	Timestamp  string            `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}
type EventStream struct {
	Tags   map[string]string `json:"tags"`
	Events []Event           `json:"events"`
}

var buffer []EventStream
var mu sync.Mutex

func AddEvent(e format.LogParts) {
	mu.Lock()
	defer mu.Unlock()

	tags := make(map[string]string)
	tags["@host"] = e["hostname"].(string)
	tags["@tag"] = e["tag"].(string)

	var found = false
	var es *EventStream
	for i, e := range buffer {
		if reflect.DeepEqual(e.Tags, tags) {
			es = &buffer[i]
			found = true
			break
		}
	}

	if !found {
		buffer = append(buffer, EventStream{
			Tags: tags,
		})
		es = &buffer[len(buffer)-1]
	}
	t := e["timestamp"].(time.Time)
	if t.After(time.Now()) {
		log.Printf("Fixing timestamp: %v", time.Since(t))
		t = time.Now().Local()
	}
	event := Event{
		Timestamp: t.Format(time.RFC3339),
		Attributes: map[string]string{
			"host": e["hostname"].(string),
			"msg":  e["content"].(string),
			"tag":  e["tag"].(string),
			"fac":  fmt.Sprintf("%v", e["facility"]),
		},
	}

	es.Events = append(es.Events, event)
	//map[client:10.0.1.1:33374 content:bound to 222.111.1.1 -- renewal in 595 seconds. facility:3 hostname:ubnt priority:30 severity:6 tag:dhclient timestamp:2019-05-11 20:26:35 +0000 UTC tls_peer:]

}

/*
[
  {
    "tags": {
      "host": "server1",
      "source": "application.log"
    },
    "events": [
      {
        "timestamp": "2016-06-06T12:00:00+02:00",
        "attributes": {
          "key1": "value1",
          "key2": "value2"
        }
      },
      {
        "timestamp": "2016-06-06T12:00:01+02:00",
        "attributes": {
          "key1": "value1"
        }
      }
    ]
  }
]
*/
func ship() error {
	mu.Lock()
	defer mu.Unlock()

	log.Printf("Sending %d event streams", len(buffer))
	if len(buffer) == 0 {
		return nil
	}
	// POST	/api/v1/ingest/humio-structured
	var body = &bytes.Buffer{}
	json.NewEncoder(body).Encode(buffer)
	buffer = nil

	req, err := http.NewRequest("POST", "https://cloud.humio.com/api/v1/ingest/humio-structured", body)
	if err != nil {
		log.Printf("JSON: %s", body.String())
		return err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("JSON: %s", body.String())
		return err
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("JSON: %s", body.String())
		log.Println(resp)
		io.Copy(os.Stdout, resp.Body)
		os.Stdout.WriteString("\n")
	}
	resp.Body.Close()
	return nil
}

func server(channel syslog.LogPartsChannel) (*syslog.Server, error) {
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.Automatic)
	server.SetHandler(handler)
	if err := server.ListenUDP("0.0.0.0:514"); err != nil {
		return nil, err
	}

	if err := server.ListenTCP("0.0.0.0:514"); err != nil {
		return nil, err
	}

	return server, server.Boot()
}

func main() {
	token = os.Getenv("HUMIO_TOKEN")
	log.SetOutput(os.Stdout)
	log.Println("Starting")

	channel := make(syslog.LogPartsChannel)
	go func(channel syslog.LogPartsChannel) {
		for logParts := range channel {
			fmt.Println(logParts)
			AddEvent(logParts)
		}
	}(channel)

	server, err := server(channel)
	if err != nil {
		log.Fatalf("boot failed: %v", err)
	}

	go func() {
		for {
			if err := ship(); err != nil {
				log.Printf("ship: %v", err)
			}
			time.Sleep(10 * time.Second)
		}
	}()

	server.Wait()
}
