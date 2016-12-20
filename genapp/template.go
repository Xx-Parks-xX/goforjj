package main

import (
	"fmt"
	"github.com/forj-oss/goforjj"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"
)

const prefix_generated_template = `// This file is autogenerated by "go generate". Do not modify it.
// It has been generated from your '{{.Yaml.Name}}.yaml' file.
// To update those structure, update the '{{.Yaml.Name}}.yaml' and run 'go generate'
`

const prefix_created_template = `// This file has been created by "go generate" as initial code. go generate will never update it, EXCEPT if you remove it.

// So, update it for your need.
`

type YamlData struct {
	Yaml      *goforjj.YamlPlugin
	Yaml_data string
}

// Define a source code model. Depending on the plugin service type, the plugin initial sources will be created from a model of sources (REST API or shell for example)
type Models struct {
	model map[string]Model
}

// Collection of source files to generate
type Model struct {
	sources    map[string]Source // key is the filename
	model_path string
}

// Core of the generated source file
// If reset = true, the generated file will regenerated each go build done.
type Source struct {
	reset    bool
	template string
	rights   os.FileMode
}

// Create a new model of sources
func (m *Models) Create(name, template_path string) *Model {
	if m.model == nil {
		m.model = make(map[string]Model)
	}

	sources := Model{sources: make(map[string]Source)}
	sources.model_path = path.Join(template_path, name)
	m.model[name] = sources
	return &sources
}

// Add a source template to the model
func (m *Model) Source(file string, rights os.FileMode, comment, tmpl_file string, reset bool) *Model {
	tmpl := path.Join(m.model_path, tmpl_file)
	if _, err := os.Stat(tmpl); os.IsNotExist(err) {
		fmt.Printf("go-forjj-generate: Warning! template source file '%s' is not accessible. \n", tmpl)
		os.Exit(1)
	}

	var tmpl_src string
	if d, err := ioutil.ReadFile(tmpl); err != nil {
		fmt.Printf("go-forjj-generate: Error! '%s' is not a readable document. %s\n", tmpl, err)
		os.Exit(1)
	} else {
		tmpl_src = string(d)
	}

	vars_regexp, _ := regexp.Compile(`.*// __MYPLUGIN: ?`)
	template_data := strings.Replace(tmpl_src, "__MYPLUGIN__", "{{ go_vars .Yaml.Name }}", -1)
	template_data = strings.Replace(template_data, "__MYPLUGINNAME__", "{{ .Yaml.Name }}", -1)
	template_data = strings.Replace(template_data, "__MYPLUGIN_UNDERSCORED__", "{{ go_vars_underscored .Yaml.Name }}", -1)
	template_data = vars_regexp.ReplaceAllLiteralString(template_data, "")
	template_data = strings.Replace(template_data, "\\\n", "", -1)
	source := Source{}
	source.reset = reset
	source.rights = rights

	switch {
	case comment == "":
		source.template = template_data
	case reset:
		file = "generated-" + file
		source.template = template_comment(prefix_generated_template, comment) + template_data
	case !reset:
		source.template = template_comment(prefix_created_template, comment) + template_data
	}

	m.sources[file] = source
	return m
}

// Set appropriate comment prefix
func template_comment(template, comment string) string {
	return strings.Replace(template, "//", comment, -1)
}

func inStringList(element string, elements ...string) string {
	for _, value := range elements {
		if element == value {
			return value
		}
	}
	return ""
}

// Create the source files from the model given.
func (m *Models) Create_model(yaml *goforjj.YamlPlugin, raw_yaml []byte, name string) {
	var yaml_data YamlData = YamlData{yaml, string(raw_yaml)}

	model, ok := m.model[name]
	if !ok {
		fmt.Printf("Invalid Model '%s' to apply.\n", name)
		os.Exit(1)
	}
	for k, v := range model.sources {
		v.apply_source(&yaml_data, k)
	}
}

func (s *Source) apply_source(yaml *YamlData, file string) {
	var tmpl *template.Template
	var err error
	var w *os.File

	if _, err = os.Stat(file); err == nil && !s.reset {
		return
	}

	// TODO: Normalize Structure name in template. For ex, - is not supported. Replace it to _ or remove it.
	tmpl, err = template.New(file).Funcs(template.FuncMap{
		"escape": func(str string) string {
			return strings.Replace(strings.Replace(str, "\"", "\\\"", -1), "\n", "\\n\" +\n   \"", -1)
		},
		"go_vars": func(str string) string {
			return strings.Replace(strings.Title(str), "-", "", -1)
		},
		"go_vars_underscored": func(str string) string {
			return strings.Replace(str, "-", "_", -1)
		},
		"has_prefix": strings.HasPrefix,
		"object_has_secure": func(object goforjj.YamlObject) bool {
			for _, flag := range object.Flags {
				if flag.Options.Secure {
					return true
				}
			}
			return false
		},
		"object_tree": func(object goforjj.YamlObject) (ret map[string]map[string]goforjj.YamlFlag) {
			ret = make(map[string]map[string]goforjj.YamlFlag)
			actions := []string{"add", "change", "remove", "rename", "list"}
			var actions_list []string

			if object.Actions != nil && len(object.Actions) > 0 {
				actions_list = object.Actions
			} else {
				actions_list = actions
			}
			for _, v := range actions_list {
				ret_a := make(map[string]goforjj.YamlFlag)
				for flag_name, flag := range object.Flags {
					if flag.Actions == nil || len(flag.Actions) == 0 {
						ret_a[flag_name] = flag
						continue
					}
					if inStringList(v, flag.Actions...) == "" {
						continue
					}
					ret_a[flag_name] = flag
				}
				if len(ret_a) > 0 {
					ret[v] = ret_a
				}
			}
			return
		},
	}).Parse(s.template)
	if err != nil {
		fmt.Printf("go-forjj-generate: Template error: %s\n", err)
		os.Exit(1)
	}

	file_path := path.Dir(file)

	if file_path != "." {
		if err := os.MkdirAll(file_path, 0755); err != nil {
			fmt.Printf("go-forjj-generate: error! Unable to create '%s' tree\n", file_path)
			os.Exit(1)
		}
	}

	w, err = os.Create(file)
	if err != nil {
		fmt.Printf("go-forjj-generate: error! '%s' is not writeable. %s\n", file, err)
		os.Exit(1)
	}
	defer w.Close()

	if err = tmpl.Execute(w, yaml); err != nil {
		fmt.Printf("go-forjj-generate: error! %s\n", err)
		os.Exit(1)
	}

	if err := os.Chmod(file, s.rights); err != nil {
		fmt.Printf("go-forjj-generate: error! Unable to set rights %d. %s\n", s.rights, err)
		os.Exit(1)
	}

	if s.reset {
		fmt.Printf("%s\n", file)
	} else {
		fmt.Printf("'%s' created. Won't be updated anymore at next go generate until file disappear.\n", file)
	}
}

const yaml_template = `---
plugin: "{{ .Yaml.Name }}"
version: "0.1"
description: "{{ .Yaml.Name }} plugin for FORJJ."
runtime:
  docker_image: "docker.hos.hpecorp.net/forjj/{{ .Yaml.Name }}"
  service_type: "REST API"
  service:
    #socket: "{{ .Yaml.Name }}.sock"
    parameters: [ "service", "start" ]
created_flag_file: "{{ "{{ .InstanceName }}" }}/forjj-{{ "{{ .Name }}" }}.yaml"
actions:
  common:
    flags:
      forjj-infra:
        help: "Name of the Infra repository to use"
      {{ .Yaml.Name }}-debug:
        help: "To activate {{ .Yaml.Name }} debug information"
      forjj-source-mount: # Used by the plugin to store plugin data in yaml. See {{ go_vars_underscored .Yaml.Name }}_plugin.go
        help: "Where the source dir is located for {{ .Yaml.Name }} plugin."
  create:
    help: "Create a {{ .Yaml.Name }} instance source code."
    flags:
      # Options related to source code
      forjj-instance-name: # Used by the plugin to store plugin data in yaml for the current instance. See {{ go_vars_underscored .Yaml.Name }}_plugin.go
        help: "Name of the {{ .Yaml.Name }} instance given by forjj."
        group: "source"
  update:
    help: "Update a {{ .Yaml.Name }} instance source code"
    flags:
      forjj-instance-name: # Used by the plugin to store plugin data in yaml for the current instance. See {{ go_vars_underscored .Yaml.Name }}_plugin.go
        help: "Name of the {{ .Yaml.Name }} instance given by forjj."
        group: "source"
  maintain:
    help: "Instantiate {{ .Yaml.Name }} thanks to source code."
`
