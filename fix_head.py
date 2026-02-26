import re

p = "/Users/eric/Documents/Gridea Pro/themes/amore-jinja2/templates/partials/head.html"
with open(p, "r", encoding="utf-8") as f:
    text = f.read()

# Fix the endfor tag
text = re.sub(r'\{%\s*endfor\s*\n\s*%\}', '{% endfor %}', text)

# Fix the cdnPrefix tag 
text = re.sub(r'\{%\s*set\s+cdnPrefix\s*=\s*[^\n]+\n\s*\([^%]+%\}', lambda m: m.group(0).replace('\n', ' '), text)

with open(p, "w", encoding="utf-8") as f:
    f.write(text)

