# BubbleFish Nexus TUI — VHS integration test
#
# Run with: vhs test.tape
#
# This tape exercises the main navigation flow:
#   1. Launch the TUI
#   2. Cycle through tabs (1-7)
#   3. Toggle help overlay
#   4. Toggle sidebar
#   5. Pause/resume auto-refresh
#   6. Quit cleanly

Output test.gif

Set Shell "bash"
Set Width 1200
Set Height 600
Set FontSize 14
Set Padding 10

# Launch the TUI dashboard.
# Requires a running daemon (nexus start) or will show "DAEMON NOT RUNNING".
Type "go run ./cmd/nexus tui"
Enter
Sleep 3s

# Tab 1: Overview (default)
Sleep 1s

# Tab 2: Audit
Type "2"
Sleep 1s

# Tab 3: Security
Type "3"
Sleep 1s

# Tab 4: Pipeline
Type "4"
Sleep 1s

# Tab 5: Conflicts
Type "5"
Sleep 1s

# Tab 6: Time-Travel
Type "6"
Sleep 1s

# Tab 7: Settings
Type "7"
Sleep 1s

# Back to Overview
Type "1"
Sleep 500ms

# Toggle help overlay (?)
Type "?"
Sleep 2s

# Close help
Type "?"
Sleep 500ms

# Toggle sidebar (H)
Type "H"
Sleep 1s

# Sidebar back on
Type "H"
Sleep 500ms

# Pause auto-refresh (ctrl+r)
Ctrl+r
Sleep 1s

# Resume
Ctrl+r
Sleep 500ms

# Quit
Type "q"
Sleep 1s
