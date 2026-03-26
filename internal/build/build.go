package build

import (
"encoding/json"
"errors"
"fmt"
"os"
"path/filepath"
)

func Build(configDir string) error {

daemonConfig, sources, destinations, err := LoadAllConfigs(configDir)
if err != nil {
return err
}

if len(destinations) == 0 {
return errors.New("build failed: no destinations configured")
}

sourceNameSet := map[string]bool{}
endpointSet := map[string]bool{}

for _, s := range sources {
if sourceNameSet[s.Name] {
return fmt.Errorf("duplicate source name: %s", s.Name)
}
sourceNameSet[s.Name] = true

if endpointSet[s.Endpoint] {
return fmt.Errorf("duplicate source endpoint: %s", s.Endpoint)
}
endpointSet[s.Endpoint] = true

if s.Auth.Type != "none" && s.Auth.Type != "api_key" {
return fmt.Errorf("invalid auth type for source %s: %s", s.Name, s.Auth.Type)
}
}

_ = daemonConfig // currently unused but loaded intentionally

primaryDest := destinations[0]

var compiledSources []CompiledSource
var compiledDestinations []CompiledDestination

compiledDestinations = append(compiledDestinations, CompiledDestination{
Name:           primaryDest.Name,
Type:           primaryDest.Type,
Filepath:       primaryDest.Filepath,
Table:          primaryDest.Table,
BaseURL:        primaryDest.BaseURL,
AuthEnvVar:     primaryDest.AuthEnvVar,
RequiredFields: primaryDest.RequiredFields,
})

enabledCount := 0

for _, s := range sources {

cs := CompiledSource{
Name:     s.Name,
Endpoint: s.Endpoint,
Color:    s.Color,
CanRead:  s.CanRead,
CanWrite: s.CanWrite,
Mapping:  s.Mapping,
Enabled:  true,
}

cs.Auth.Type = s.Auth.Type
cs.Auth.Header = s.Auth.Header
cs.Auth.EnvVar = s.Auth.EnvVar

if s.CanWrite {

for _, required := range primaryDest.RequiredFields {
if _, ok := s.Mapping[required]; !ok {
cs.Enabled = false
cs.ErrorMessage = fmt.Sprintf("missing required field \"%s\"", required)
break
}
}
}

if cs.Enabled {
enabledCount++
}

compiledSources = append(compiledSources, cs)
}

if enabledCount == 0 {
return errors.New("build failed: zero enabled sources")
}

compiledDir := filepath.Join(configDir, "compiled")
err = os.MkdirAll(compiledDir, 0755)
if err != nil {
return err
}

sourcesJSON, _ := json.MarshalIndent(compiledSources, "", "  ")
destJSON, _ := json.MarshalIndent(compiledDestinations, "", "  ")

err = os.WriteFile(filepath.Join(compiledDir, "sources.json"), sourcesJSON, 0644)
if err != nil {
return err
}

err = os.WriteFile(filepath.Join(compiledDir, "destinations.json"), destJSON, 0644)
if err != nil {
return err
}

return nil
}
