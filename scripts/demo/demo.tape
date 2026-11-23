# Records the README demo video into docs/media/demo.mp4 (`make media` also
# converts it to demo.gif). See scripts/demo/README.md.
Output "docs/media/demo.mp4"
Source "scripts/demo/common.tape"

Hide
Type "export PATH=$PWD/scripts/demo/bin:$PATH PGPASSWORD=demo && clear" Enter
Show

Sleep 500ms
Type "pg-monk -H pgmonk-pg -U postgres -d shop"
Sleep 600ms
Enter
Sleep 6s

# Sessions — color-coded by state, sorted
Type "2"
Sleep 3s
Type "s"
Sleep 2s

# Top queries from pg_stat_statements, open the detail panel
Type "3"
Sleep 2500ms
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Enter
Sleep 3s
Escape
Sleep 500ms

# Table health
Type "4"
Sleep 3s

# Blocking chains
Type "5"
Sleep 4s

# Per-client drill-down
Type "6"
Sleep 2s
Enter
Sleep 3s
Escape
Sleep 500ms

# Rule-based insights
Type "7"
Sleep 5s

Type "q"
Sleep 1s

Hide
Type "clear" Enter
Show
Type "pg-monk diagnose -H pgmonk-pg -U postgres -d shop"
Sleep 500ms
Enter
Sleep 8s
