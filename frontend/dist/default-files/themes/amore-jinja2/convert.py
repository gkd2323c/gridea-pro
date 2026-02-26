import os
import re
import glob

def convert_ejs_to_jinja2(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    # Replacements list
    # 1. Variables
    content = re.sub(r'<%=\s*(.*?)\s*%>', r'{{ \1 }}', content)
    # Output raw
    content = re.sub(r'<%\-\s*include\([\'"](.*?)[\'"]\)\s*%>', lambda m: f'{{% include "{m.group(1)}.html" %}}', content)
    content = re.sub(r'<%\-\s*(.*?)\s*%>', r'{{ \1 | safe }}', content)
    
    # 2. Conditionals
    content = re.sub(r'<%\s*if\s*\((.*?)\)\s*\{\s*%>', r'{% if \1 %}', content)
    content = re.sub(r'<%\s*\} else if\s*\((.*?)\)\s*\{\s*%>', r'{% elif \1 %}', content)
    content = re.sub(r'<%\s*\} else \{\s*%>', r'{% else %}', content)
    
    # 3. Loops (e.g. posts.forEach(function(post) {) or tags.forEach(tag => {
    # Let's match `<% xxx.forEach(function(yyy) { %>` or `<% xxx.forEach(yyy => { %>`
    def loop_repl(m):
        iterable = m.group(1)
        item = m.group(2)
        return f'{{% for {item} in {iterable} %}}'
    
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.forEach\(\s*(?:function\s*\([a-zA-Z0-9_\.]+\)|([a-zA-Z0-9_\.]+))\s*(?:=>)?\s*\{\s*%>', loop_repl, content)
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.forEach\(function\s*\(([a-zA-Z0-9_\.]+)\)\s*\{\s*%>', loop_repl, content)
    
    # Fix site/theme references often used in EJS
    content = content.replace('.forEach(function(', '.forEach(')  # simplified
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.forEach\(([a-zA-Z0-9_\.]+)\s*=>\s*\{\s*%>', r'{% for \2 in \1 %}', content)

    # A more generic loop match for 'menus.forEach(menu => {'
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.forEach\(\s*(?:function\s*\(\s*|\s*)([a-zA-Z0-9_\.]+)\s*(?:\)\s*=>|=>|\))\s*\{\s*%>', r'{% for \2 in \1 %}', content)

    # 4. Closing bracs `<% } %>` - we need to determine if it's closing an if or a loop.
    # This is tricky without a stack parser. Let's do a simple stack parser.
    # We will tokenize the `{% ... %}` blocks that we just created and the remaining `<% } %>`
    tokens = re.split(r'({%.*?%}|<%\s*\}\s*%>)', content)
    
    stack = []
    for i, token in enumerate(tokens):
        if token.startswith('{% if'):
            stack.append('if')
        elif token.startswith('{% for'):
            stack.append('for')
        elif re.match(r'<%\s*\}\s*%>', token):
            if stack:
                top = stack.pop()
                tokens[i] = f'{{% end{top} %}}'
            else:
                tokens[i] = '{% endif %}' # fallback

    content = "".join(tokens)

    # Clean up and simple fixes
    content = content.replace('post.tags.length', 'post.tags | length')
    content = content.replace('site.customConfig', 'theme_config')

    # Specific replacements
    content = content.replace('menu.openType === \'External\'', 'menu.openType == "External"')
    content = content.replace('menu.openType === \'Internal\'', 'menu.openType == "Internal"')

    # URL path fix
    content = content.replace("themeConfig.siteName", "config.siteName")
    content = content.replace("themeConfig.domain", "config.domain")

    # output
    new_filepath = filepath[:-4] + '.html'
    with open(new_filepath, 'w', encoding='utf-8') as f:
        f.write(content)
    
    os.remove(filepath)
    print(f"Converted {filepath} -> {new_filepath}")

base_path = "/Volumes/Work/VibeCoding/Gridea Pro/frontend/public/default-files/themes/amore-jinja2/templates"

for root, dirs, files in os.walk(base_path):
    for file in files:
        if file.endswith('.ejs'):
            filepath = os.path.join(root, file)
            convert_ejs_to_jinja2(filepath)
