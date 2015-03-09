package haproxy

import (
	"io"
	"log"
	"os/exec"
	"strconv"
	"text/template"
	"time"

	"github.com/newrelic/bosun/service"
	"github.com/newrelic/bosun/services_state"
)

type portset map[string]struct{}
type portmap map[string]portset

type HAproxy struct {
	ReloadCmd string
	VerifyCmd string
	BindIP string
}

func New() *HAproxy {
	proxy := HAproxy{
		ReloadCmd: "haproxy -f /etc/haproxy.cfg -p /var/run/haproxy.pid -sf $(cat /var/run/haproxy.pid)",
		VerifyCmd: "",
	}

	return &proxy
}

func (h *HAproxy) makePortmap(services map[string][]*service.Service) portmap {
	ports := make(portmap)

	for name, svcList := range services {
        if _, ok := ports[name]; !ok {
			ports[name] = make(portset, 5)
		}

		for _, service := range svcList {
			for _, svcPort := range service.Ports {
				if svcPort.Type == "tcp" {
					ports[name][strconv.FormatInt(svcPort.Port, 10)] = struct{}{}
				}
			}
		}
	}

	return ports
}

func (h *HAproxy) WriteConfig(state *services_state.ServicesState, output io.Writer) {
	services := state.ByService()
	ports    := h.makePortmap(services)

	data := struct {
		Services map[string][]*service.Service
	}{
		Services: services,
	}

    funcMap := template.FuncMap{
		"now": time.Now().UTC,
		"getPorts": func(k string) []string {
			var keys []string
			for key, _ := range ports[k] {
				keys = append(keys, key)
			}
			return keys
		},
		"bindIP": func() string { return h.BindIP },
    }

	t, err := template.New("haproxy").Funcs(funcMap).ParseFiles("../views/haproxy.cfg")
	if err != nil {
		log.Printf("Error Parsing template hapxroxy.cfg: %s\n", err.Error())
		return
	}
	t.ExecuteTemplate(output, "haproxy.cfg", data)
}

func (h *HAproxy) Reload() error {
	cmd := exec.Command("/bin/bash", "-c", h.ReloadCmd)
	return cmd.Run()
}

func (h *HAproxy) Verify() error {
	cmd := exec.Command("/bin/bash", "-c", h.VerifyCmd)
	return cmd.Run()
}