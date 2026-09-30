package appconfig

import (
	"errors"
	"path/filepath"

	"teamcross/internal/service"
)

type Config struct {
	Directory string
	Name      string
}

// The builder selects the profile. Preview cannot silently adopt installed
// App data, including when a caller supplies a filesystem alias.
func Resolve(profile, data, home string) (Config, error) {
	if profile != "preview" && profile != "release" {
		return Config{}, errors.New("unknown desktop build profile")
	}
	if !filepath.IsAbs(home) {
		return Config{}, errors.New("cannot resolve the home directory")
	}
	production, err := service.Normalize(filepath.Join(home, "Library", "Application Support", "Team Cross Next"))
	if err != nil {
		return Config{}, err
	}
	if data == "" {
		if profile == "preview" {
			return Config{}, errors.New("Desktop Preview requires an isolated --data-dir")
		}
		data = production
	}
	directory, err := service.Normalize(data)
	if err != nil {
		return Config{}, err
	}
	name := "Team Cross"
	if profile == "preview" {
		if directory == production {
			return Config{}, errors.New("Desktop Preview cannot use the installed Core directory")
		}
		name = "Team Cross Desktop Preview"
	}
	return Config{Directory: directory, Name: name}, nil
}
