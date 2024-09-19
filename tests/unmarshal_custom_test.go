package koanf

import (
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestUnmarshalDefaultTag(t *testing.T) {
	type configType struct {
		LogLevel string `koanf:"log_level" default:"Warn"`
		Port     int    `koanf:"port" default:"4000"`
	}

	var config configType
	unmarshalConf := koanf.UnmarshalConf{Tag: "koanf"}

	const delim = "."
	const path = ""

	k := koanf.New(delim)

	err := k.Load(confmap.Provider(map[string]interface{}{
		//"port":      "4000",
		"log_level": "Info",
	}, "."), nil)

	assert.Nilf(t, err, "failed to load: %v", err)

	err = k.UnmarshalWithConf2(path, &config, unmarshalConf)
	assert.Nilf(t, err, "failed to unmarshal: %v", err)

	assert.Equal(t, "Info", config.LogLevel)
	assert.Equal(t, 4000, config.Port)
}

func TestUnmarshalRequiredTag(t *testing.T) {
	type configType struct {
		ApiKey string `koanf:"api_key" required:"true"`
	}

	var config configType
	unmarshalConf := koanf.UnmarshalConf{Tag: "koanf"}

	const delim = "."
	const path = ""

	k := koanf.New(delim)

	err := k.Load(confmap.Provider(map[string]interface{}{}, "."), nil)

	assert.Nilf(t, err, "failed to load: %v", err)

	err = k.UnmarshalWithConf2(path, &config, unmarshalConf)
	assert.NotNilf(t, err, "expected error for required field, got nil")

	assert.Equal(t, "", config.ApiKey)
}
