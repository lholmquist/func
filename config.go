package function

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

// ConfigFile is the name of the config's serialized form.
const ConfigFile = "func.yaml"

type Volumes []Volume
type Volume struct {
	Secret    *string `yaml:"secret,omitempty"`
	ConfigMap *string `yaml:"configMap,omitempty"`
	Path      *string `yaml:"path"`
}

type Envs []Env
type Env struct {
	Name  *string `yaml:"name,omitempty"`
	Value *string `yaml:"value"`
}

// Config represents the serialized state of a Function's metadata.
// See the Function struct for attribute documentation.
type config struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace"`
	Runtime     string            `yaml:"runtime"`
	Image       string            `yaml:"image"`
	ImageDigest string            `yaml:"imageDigest"`
	Trigger     string            `yaml:"trigger"`
	Builder     string            `yaml:"builder"`
	BuilderMap  map[string]string `yaml:"builderMap"`
	Volumes     Volumes           `yaml:"volumes"`
	Envs        Envs              `yaml:"envs"`
	Annotations map[string]string `yaml:"annotations"`
	// Add new values to the toConfig/fromConfig functions.
}

// newConfig returns a Config populated from data serialized to disk if it is
// available.  Errors are returned if the path is not valid, if there are
// errors accessing an extant config file, or the contents of the file do not
// unmarshall.  A missing file at a valid path does not error but returns the
// empty value of Config.
func newConfig(root string) (c config, err error) {
	filename := filepath.Join(root, ConfigFile)
	if _, err = os.Stat(filename); err != nil {
		// do not consider a missing config file an error.  Just return.
		if os.IsNotExist(err) {
			err = nil
		}
		return
	}
	bb, err := ioutil.ReadFile(filename)
	if err != nil {
		return
	}

	errMsg := ""
	errMsgHeader := "'func.yaml' config file is not valid:\n"
	errMsgReg := regexp.MustCompile("not found in type .*")

	// Let's try to unmarshal the config file, any fields that are found
	// in the data that do not have corresponding struct members, or mapping
	// keys that are duplicates, will result in an error.
	err = yaml.UnmarshalStrict(bb, &c)
	if err != nil {
		errMsg = err.Error()

		if strings.HasPrefix(errMsg, "yaml: unmarshal errors:") {
			errMsg = errMsgReg.ReplaceAllString(errMsg, "is not valid")
			errMsg = strings.Replace(errMsg, "yaml: unmarshal errors:\n", errMsgHeader, 1)
		} else if strings.HasPrefix(errMsg, "yaml:") {
			errMsg = errMsgReg.ReplaceAllString(errMsg, "is not valid")
			errMsg = strings.Replace(errMsg, "yaml: ", errMsgHeader+"  ", 1)
		}
	}

	// Let's check that all entries in `volumes` and `envs` contain all required fields
	volumesErrors := validateVolumes(c.Volumes)
	envsErrors := ValidateEnvs(c.Envs)
	if len(volumesErrors) > 0 || len(envsErrors) > 0 {
		// if there aren't any previously reported errors, we need to set the error message header first
		if errMsg == "" {
			errMsg = errMsgHeader
		} else {
			// if there are some previously reporeted errors, we need to indent them
			errMsg = errMsg + "\n"
		}

		// lets make the error message a little bit nice -> indent each error message
		for i := range volumesErrors {
			volumesErrors[i] = "  " + volumesErrors[i]
		}
		for i := range envsErrors {
			envsErrors[i] = "  " + envsErrors[i]
		}

		errMsg = errMsg + strings.Join(volumesErrors, "\n")
		// we have errors from both volumes and envs sections -> let's make sure they are both indented
		if len(volumesErrors) > 0 && len(envsErrors) > 0 {
			errMsg = errMsg + "\n"
		}
		errMsg = errMsg + strings.Join(envsErrors, "\n")
	}

	if errMsg != "" {
		err = errors.New(errMsg)
	}

	return
}

// fromConfig returns a Function populated from config.
// Note that config does not include ancillary fields not serialized, such as Root.
func fromConfig(c config) (f Function) {
	return Function{
		Name:        c.Name,
		Namespace:   c.Namespace,
		Runtime:     c.Runtime,
		Image:       c.Image,
		ImageDigest: c.ImageDigest,
		Trigger:     c.Trigger,
		Builder:     c.Builder,
		BuilderMap:  c.BuilderMap,
		Volumes:     c.Volumes,
		Envs:        c.Envs,
		Annotations: c.Annotations,
	}
}

// toConfig serializes a Function to a config object.
func toConfig(f Function) config {
	return config{
		Name:        f.Name,
		Namespace:   f.Namespace,
		Runtime:     f.Runtime,
		Image:       f.Image,
		ImageDigest: f.ImageDigest,
		Trigger:     f.Trigger,
		Builder:     f.Builder,
		BuilderMap:  f.BuilderMap,
		Volumes:     f.Volumes,
		Envs:        f.Envs,
		Annotations: f.Annotations,
	}
}

// writeConfig for the given Function out to disk at root.
func writeConfig(f Function) (err error) {
	path := filepath.Join(f.Root, ConfigFile)
	c := toConfig(f)
	var bb []byte
	if bb, err = yaml.Marshal(&c); err != nil {
		return
	}
	return ioutil.WriteFile(path, bb, 0644)
}

// validateVolumes checks that input Volumes are correct and contain all necessary fields.
// Returns array of error messages, empty if none
//
// Allowed settings:
// - secret: example-secret              		# mount Secret as Volume
// 	 path: /etc/secret-volume
// - configMap: example-configMap              	# mount ConfigMap as Volume
// 	 path: /etc/configMap-volume
func validateVolumes(volumes Volumes) (errors []string) {

	for i, vol := range volumes {
		if vol.Secret != nil && vol.ConfigMap != nil {
			errors = append(errors, fmt.Sprintf("volume entry #%d is not properly set, both secret '%s' and configMap '%s' can not be set at the same time",
				i, *vol.Secret, *vol.ConfigMap))
		} else if vol.Path == nil && vol.Secret == nil && vol.ConfigMap == nil {
			errors = append(errors, fmt.Sprintf("volume entry #%d is not properly set", i))
		} else if vol.Path == nil {
			if vol.Secret != nil {
				errors = append(errors, fmt.Sprintf("volume entry #%d is missing path field, only secret '%s' is set", i, *vol.Secret))
			} else if vol.ConfigMap != nil {
				errors = append(errors, fmt.Sprintf("volume entry #%d is missing path field, only configMap '%s' is set", i, *vol.ConfigMap))
			}
		} else if vol.Path != nil && vol.Secret == nil && vol.ConfigMap == nil {
			errors = append(errors, fmt.Sprintf("volume entry #%d is missing secret or configMap field, only path '%s' is set", i, *vol.Path))
		}
	}

	return
}

// ValidateEnvs checks that input Envs are correct and contain all necessary fields.
// Returns array of error messages, empty if none
//
// Allowed settings:
// - name: EXAMPLE1                					# ENV directly from a value
//   value: value1
// - name: EXAMPLE2                 				# ENV from the local ENV var
//   value: {{ env.MY_ENV }}
// - name: EXAMPLE3
//   value: {{ secret.secretName.key }}   			# ENV from a key in secret
// - value: {{ secret.secretName }}          		# all key-pair values from secret are set as ENV
// - name: EXAMPLE4
//   value: {{ configMap.configMapName.key }}   	# ENV from a key in configMap
// - value: {{ configMap.configMapName }}          	# all key-pair values from configMap are set as ENV
func ValidateEnvs(envs Envs) (errors []string) {

	// there could be '-' char in the secret/configMap name, but not in the key
	regWholeSecret := regexp.MustCompile(`^{{\s*secret\.(?:\w|['-]\w)+\s*}}$`)
	regKeyFromSecret := regexp.MustCompile(`^{{\s*secret\.(?:\w|['-]\w)+\.\w+\s*}}$`)
	regWholeConfigMap := regexp.MustCompile(`^{{\s*configMap\.(?:\w|['-]\w)+\s*}}$`)
	regKeyFromConfigMap := regexp.MustCompile(`^{{\s*configMap\.(?:\w|['-]\w)+\.\w+\s*}}$`)
	regLocalEnv := regexp.MustCompile(`^{{\s*env\.(\w+)\s*}}$`)

	for i, env := range envs {
		if env.Name == nil && env.Value == nil {
			errors = append(errors, fmt.Sprintf("env entry #%d is not properly set", i))
		} else if env.Value == nil {
			errors = append(errors, fmt.Sprintf("env entry #%d is missing value field, only name '%s' is set", i, *env.Name))
		} else if env.Name == nil {
			// all key-pair values from secret are set as ENV; {{ secret.secretName }} or {{ configMap.configMapName }}
			if !regWholeSecret.MatchString(*env.Value) && !regWholeConfigMap.MatchString(*env.Value) {
				errors = append(errors, fmt.Sprintf("env entry #%d has invalid value field set, it has '%s', but allowed is only '{{ secret.secretName }}' or '{{ configMap.configMapName }}'",
				 i, *env.Value))
			}
		} else {
			if strings.HasPrefix(*env.Value, "{{") {
				// ENV from the local ENV var; {{ env.MY_ENV }}
				// or
				// ENV from a key in secret/configMap;  {{ secret.secretName.key }} or {{ configMap.configMapName.key }}
				if !regLocalEnv.MatchString(*env.Value) && !regKeyFromSecret.MatchString(*env.Value) && !regKeyFromConfigMap.MatchString(*env.Value) {
					errors = append(errors,
						fmt.Sprintf(
							"env entry #%d with name '%s' has invalid value field set, it has '%s', but allowed is only '{{ env.MY_ENV }}', '{{ secret.secretName.key }}' or '{{ configMap.configMapName.key }}'",
							i, *env.Name, *env.Value))
				}
			}

		}
	}

	return
}
