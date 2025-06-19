package controller

import (
	"bytes"
	"embed"
	_ "embed"
	"encoding/base64"
	"io/fs"
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

type TemplatedResourceEditor struct {
	templateYaml string
	parsedYaml   []byte
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
	"base64": base64.StdEncoding.EncodeToString,
}

func (te *TemplatedResourceEditor) getTemplatedYaml() ([]byte, error) {
	if te.parsedYaml != nil {
		return te.parsedYaml, nil
	}
	tmpl, err := template.New("").Funcs(formatFuncs).Parse(te.templateYaml)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, te.templateData); err != nil {
		return nil, err
	}

	te.parsedYaml = buf.Bytes()
	return te.parsedYaml, nil
}

func (te *TemplatedResourceEditor) getInitialResourceValue() (client.Object, error) {
	parsed, err := te.getTemplatedYaml()
	if err != nil {
		return nil, err
	}
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(parsed, nil, nil)
	if err != nil {
		return nil, err
	}
	return obj.(client.Object), nil
}

func (te *TemplatedResourceEditor) setResourceValue(
	r Reconciler, resource metav1.Object, obj client.Object) error {
	tmpl, err := template.New("").Funcs(formatFuncs).Parse(te.templateYaml)
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
	tmpl, err := template.New("").Funcs(formatFuncs).Parse(te.templateYaml)
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

// readManifestsFromFS reads all the yaml templates out of a given directory in an embed.FS
func readManifestsFromFS(embedFS embed.FS, baseDir string) (yamlTemplates []string, err error) {
	err = fs.WalkDir(embedFS, baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		} else if d.IsDir() {
			return nil
		}

		content, err := embedFS.ReadFile(path)
		if err != nil {
			return err
		}

		yamlTemplates = append(yamlTemplates, string(content))
		return nil
	})

	return yamlTemplates, err
}
