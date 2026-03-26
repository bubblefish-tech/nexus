package main

import (
"encoding/json"
"fmt"
"os"
"path/filepath"

"github.com/shawnsammartano-hub/bubblefish/internal/build"
)

func main() {

if len(os.Args) < 2 {
fmt.Println("Usage: bubblefish <command>")
return
}

switch os.Args[1] {

case "build":
runBuild()

default:
fmt.Println("Unknown command")
}
}

func runBuild() {

home, err := os.UserHomeDir()
if err != nil {
fmt.Println("Could not resolve home directory")
return
}

configDir := filepath.Join(home, ".bubblefish")

err = build.Build(configDir)
if err != nil {
fmt.Println(err)
return
}

compiledDir := filepath.Join(configDir, "compiled")

sourcesData, _ := os.ReadFile(filepath.Join(compiledDir, "sources.json"))

var compiledSources []build.CompiledSource
json.Unmarshal(sourcesData, &compiledSources)

enabled := 0
disabled := 0

fmt.Println("Build complete.")

for _, s := range compiledSources {
if s.Enabled {
enabled++
} else {
disabled++
}
}

fmt.Printf("  Sources enabled: %d\n", enabled)
fmt.Printf("  Sources disabled: %d\n", disabled)

for _, s := range compiledSources {
if !s.Enabled {
fmt.Printf("    - %s: %s\n", s.Name, s.ErrorMessage)
}
}

destData, _ := os.ReadFile(filepath.Join(compiledDir, "destinations.json"))

var compiledDest []build.CompiledDestination
json.Unmarshal(destData, &compiledDest)

if len(compiledDest) > 0 {
fmt.Printf("  Destinations: %d (%s)\n", len(compiledDest), compiledDest[0].Name)
}
}
