# Captures one PNG per view into docs/media/. See scripts/demo/README.md.
Output "/tmp/screenshots.gif"
Source "scripts/demo/common.tape"

Hide
Type "export PATH=$PWD/scripts/demo/bin:$PATH PGPASSWORD=demo && clear" Enter
Type "pg-monk -H pgmonk-pg -U postgres -d shop" Enter
Sleep 6s
Show

Sleep 1s
Screenshot "docs/media/dashboard.png"
Sleep 500ms

Type "2"
Sleep 2s
Screenshot "docs/media/sessions.png"
Sleep 500ms

Type "3"
Sleep 2s
Screenshot "docs/media/queries.png"
Sleep 500ms

Type "4"
Sleep 2s
Screenshot "docs/media/tables.png"
Sleep 500ms

Type "5"
Sleep 2s
Screenshot "docs/media/locks.png"
Sleep 500ms

Type "6"
Sleep 2s
Screenshot "docs/media/clients.png"
Sleep 500ms

Type "7"
Sleep 2s
Screenshot "docs/media/insights.png"
Sleep 500ms

Type "q"
Sleep 1s
Hide
Type "clear" Enter
Show
Type "pg-monk diagnose -H pgmonk-pg -U postgres -d shop" Enter
Sleep 6s
Screenshot "docs/media/diagnose.png"
Sleep 500ms
