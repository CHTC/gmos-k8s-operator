package controller

import (
	"bytes"
	_ "embed"
	"strings"

	"text/template"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sYaml "sigs.k8s.io/yaml"

	"k8s.io/client-go/kubernetes/scheme"
)

// Struct for string formatting the Prometheus Config file
const PROMETHEUS = "prometheus"
const PUSHGATEWAY = "prometheus-pushgateway"
const PROMETHEUS_PORT = 9090
const PUSHGATEWAY_PORT = 9091

//go:embed manifests/prometheus-deployment.yaml
var promDeployYaml string

//go:embed manifests/prometheus-config.yaml
var promConfigYaml string

//go:embed manifests/prometheus-pushgateway.yaml
var promPushgateway string

//go:embed manifests/prometheus-pushgateway-service.yaml
var promPushgatewayService string

//go:embed manifests/prometheus-service.yaml
var promService string

type TemplatedResourceEditor struct {
	templateYaml string
	templateData any
}

var formatFuncs map[string]interface{} = map[string]interface{}{
	"toYaml": func(input interface{}) (string, error) {
		val, err := k8sYaml.Marshal(input)
		return string(val), err
	},
	"nindent": func(indentCount int, input string) string {
		indent := strings.Repeat(" ", indentCount)
		lines := strings.SplitAfter(input, "\n")
		return strings.Join(lines, indent)
	},
}

func (te *TemplatedResourceEditor) getInitialResourceValue() (client.Object, error) {
	tmpl, err := template.New("prometheusDeployment").Funcs(formatFuncs).Parse(te.templateYaml)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, te.templateData); err != nil {
		return nil, err
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(buf.Bytes(), nil, nil)
	return obj.(client.Object), err
}

func (te *TemplatedResourceEditor) setResourceValue(
	r Reconciler, resource metav1.Object, obj client.Object) error {
	tmpl, err := template.New("prometheusDeployment").Funcs(formatFuncs).Parse(te.templateYaml)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, te.templateData); err != nil {
		return err
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	_, _, err = decode(buf.Bytes(), nil, obj)
	return err
}

func (te *TemplatedResourceEditor) updateResourceValue(r Reconciler, obj client.Object) (bool, error) {
	tmpl, err := template.New("prometheusDeployment").Funcs(formatFuncs).Parse(te.templateYaml)
	if err != nil {
		return false, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, te.templateData); err != nil {
		return false, err
	}
	decode := scheme.Codecs.UniversalDeserializer().Decode
	_, _, err = decode(buf.Bytes(), nil, obj)
	return true, err
}
