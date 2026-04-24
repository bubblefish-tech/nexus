Output scripts/vhs/out/T1_1_empty_states.gif
Set FontSize 14
Set Width 1600
Set Height 1000
Set Theme "Dracula"
Set Shell "cmd"
Set TypingSpeed 50ms

Env NEXUS_API_URL "http://localhost:8080"

# Intentionally NO admin token — tests EmptyStateFeatureGated rendering
Type "D:\bubblefish\nexus\nexus.exe tui"
Enter
Sleep 4s
Type "2"
Sleep 2s
Screenshot scripts/vhs/out/T1_1_memory_empty.png
Type "7"
Sleep 2s
Screenshot scripts/vhs/out/T1_1_gov_empty.png
Type "q"
Sleep 500ms
