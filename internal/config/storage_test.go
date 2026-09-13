package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
)

func TestStorageProjectionAndReplacement(t *testing.T) {
	t.Parallel()
	first, err := config.ParseStorage([]byte(`[storage]
driver='overlay'
graphroot='$HOME/store'
runroot='/run/store'
rootless_storage_path='/srv/$UID/store'
driver_priority=['overlay','vfs']
transient_store=true
[storage.options]
mount_program='/usr/bin/fuse-overlayfs'
additionalimagestores=['/srv/images']
additionallayerstores=['/srv/layers:ref']
force_mask=493
[storage.options.overlay]
mount_program='/opt/helper'
ignore_chown_errors='true'
[storage.options.pull_options]
private='synthetic-secret'
`))
	if err != nil {
		t.Fatal(err)
	}
	if first.Driver == nil || *first.Driver != "overlay" || *first.GraphRoot != "$HOME/store" ||
		first.Options["mount_program"] != "/usr/bin/fuse-overlayfs" || first.Options["overlay.mount_program"] != "/opt/helper" ||
		first.Options["force_mask"] != "755" || first.UnprojectedFieldCount == 0 {
		t.Fatalf("lost storage projection: %+v", first)
	}
	projected := config.ProjectStorage(first, "first")
	if projected.Driver.SourceID != "first" || projected.AdditionalImageStores.Origins[0] != "first" {
		t.Fatal("lost field provenance")
	}
	second, err := config.ParseStorage([]byte("[storage]\ndriver='vfs'"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := config.ProjectStorage(second, "second")
	if replacement.GraphRoot != nil || replacement.AdditionalImageStores != nil || len(replacement.Options) != 0 {
		t.Fatal("storage replacement retained previous file fields")
	}
	first.Options["mount_program"] = "/changed"
	if projected.Options["mount_program"].Value == "/changed" {
		t.Fatal("projection borrowed source map")
	}
}

func TestStorageUnsupportedAndMalformedFields(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text string
		want error
	}{
		{"[storage]\ndriver=3", config.ErrConfigFieldInvalid},
		{"[storage]\ndriver='odd driver'", config.ErrConfigFieldUnsupported},
		{"[storage]\ngraphroot='relative'", config.ErrConfigFieldUnsupported},
		{"[storage]\ntransient_store='true'", config.ErrConfigFieldInvalid},
		{"[storage]\ndriver_priority=['overlay',{append=true}]", config.ErrConfigFieldInvalid},
		{"[storage.options]\nadditionalimagestores=['relative']", config.ErrConfigFieldUnsupported},
		{"[storage.options]\nadditionallayerstores=['/data:secret']", config.ErrConfigFieldUnsupported},
		{"[storage.options]\nmount_program=3", config.ErrConfigFieldInvalid},
		{"[storage.options.overlay]\nignore_chown_errors=3", config.ErrConfigFieldInvalid},
		{"[storage.options]\nforce_mask=-1", config.ErrConfigFieldInvalid},
		{"[storage.options]\nforce_mask=4294967296", config.ErrConfigFieldUnsupported},
		{"[storage.options]\nadditionalimagestores=['/a',{append=true}]", config.ErrConfigFieldInvalid},
		{"[storage]\ndriver='vfs'\ndriver='overlay'", config.ErrConfigMalformed},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseStorage([]byte(test.text))
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("raw value leaked")
			}
		})
	}
}

func TestStoragePathExpansionUsesOnlyDeclaredValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input, want string
		invalid     bool
	}{
		{"$HOME/store", "/home/target/store", false},
		{"${XDG_DATA_HOME}/store", "/data/store", false},
		{"/srv/$UID/store", "/srv/1001/store", false},
		{"/srv/$UID_suffix", "/srv/1001_suffix", false},
		{"$NOT_DECLARED/store", "/store", false},
		{"$NOT_DECLARED", "", true},
		{"/data/../store", "", true},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := config.ExpandStoragePath(test.input, map[string]string{"HOME": "/home/target", "XDG_DATA_HOME": "/data"}, 1001)
			if (err != nil) != test.invalid || got != test.want {
				t.Fatalf("path=%q error=%v; want=%q invalid=%t", got, err, test.want, test.invalid)
			}
		})
	}
}

func TestStorageRecognizedOptionShapes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text    string
		invalid bool
	}{
		{"", false}, {"storage=3", true}, {"[storage]\nimagestore='/images'\nunknown='not-retained'", false},
		{"[storage]\ngraphroot=\"/bad\\u0007path\"", true}, {"[storage]\ngraphroot='~/store'", true},
		{"[storage]\ndriver_priority=3", true}, {"[storage]\ndriver_priority=['not a driver']", true},
		{"[storage]\ndriver_priority=[" + strings.Repeat("'vfs',", 65) + "]", true},
		{"[storage]\noptions=3", true}, {"[storage.options]\noverlay=3", true},
		{"[storage.options]\nmount_program='relative'", true},
		{"[storage.options]\nadditionalimagestores=3", true},
		{"[storage.options.overlay]\nforce_mask='broken'", false},
		{"[storage.options.overlay]\nforce_mask='private'\nuse_composefs='true'\nskip_mount_home=''", false},
		{"[storage.options.overlay]\nforce_mask='755'", false},
		{"[storage.options.vfs]\nignore_chown_errors='true'\nunknown='not-retained'", false},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			_, err := config.ParseStorage([]byte(test.text))
			if (err != nil) != test.invalid {
				t.Fatal(err)
			}
		})
	}
}

func TestStorageInvalidStringOptionsAreRedactedUntilSelection(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"ignore_chown_errors", "skip_mount_home", "force_mask", "use_composefs"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			layer, err := config.ParseStorage([]byte("[storage.options.overlay]\n" + key + "='synthetic-secret'"))
			if err != nil {
				t.Fatal(err)
			}
			value := config.ProjectStorage(layer, "source").Options["overlay."+key]
			if !value.Invalid || value.Value != "" || value.SourceID != "source" {
				t.Fatal("invalid value or provenance was lost or exposed")
			}
		})
	}
}
