# Captures the AI panel on the Insights tab into docs/media/ai-insights.png.
# Needs a model server reachable from the VHS container (see `make media-ai`).
Output "/tmp/ai.mp4"
Source "scripts/demo/common.tape"
Set Framerate 5

Hide
Type "export PATH=$PWD/scripts/demo/bin:$PATH PGPASSWORD=demo && clear" Enter
Type "pg-monk --ai --ai-url $AI_URL --ai-model $AI_MODEL -H pgmonk-pg -U postgres -d shop" Enter
Sleep 5s
Type "7"
Show
Sleep 90s
Screenshot "docs/media/ai-insights.png"
Sleep 1s
Type "q"
