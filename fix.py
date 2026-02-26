import os

paths = [
    "/Users/eric/Documents/Gridea Pro/themes/amore-jinja2/templates/partials/head.html",
    "/Volumes/Work/VibeCoding/Gridea Pro/frontend/public/default-files/themes/amore-jinja2/templates/partials/head.html"
]

for p in paths:
    with open(p, "r", encoding="utf-8") as f:
        content = f.read()
    
    # Very literal replacement exactly around index0
    content = content.replace("{% if loop.index0\n< 20 %}", "{% if loop.index0 < 20 %}")
    content = content.replace("{% if loop.index0\r\n< 20 %}", "{% if loop.index0 < 20 %}")
    
    with open(p, "w", encoding="utf-8") as f:
        f.write(content)
