import os
import re

def fix_remaining(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    # `<% }); %>` or `<% }) %>`
    content = re.sub(r'<%\s*\}\)?;\s*%>', r'{% endfor %}', content)
    content = re.sub(r'<%\s*\}\)\s*%>', r'{% endfor %}', content)

    # `<% menus.forEach(menu, index) { %>` -> `{% for menu in menus %}`
    # Note: Jinja uses loop.index0 instead of index
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.forEach\(([a-zA-Z0-9_]+),\s*[a-zA-Z0-9_]+\)\s*\{\s*%>', r'{% for \2 in \1 %}', content)

    # `<% ['github', ... ].forEach((item)=> { %>`
    content = re.sub(r'<%\s*(\[.*?\])\.forEach\(\(([a-zA-Z0-9_]+)\)\s*=>\s*\{\s*%>', r'{% for \2 in \1 %}', content)
    
    # `<% posts.slice(0, 10).forEach(post) { %>`
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.slice\(([0-9]+),\s*([0-9]+)\)\.forEach\(([a-zA-Z0-9_]+)\)\s*\{\s*%>', r'{% for \4 in \1 | slice:"\2:\3" %}', content)
    # wait, slice in pongo2 is slice:"0:5" but returning a list of lists or just string? For list of posts, it might be better to just leave it as posts or use slice in another way. Let's rely on a simple slice syntax. Since `posts` is just evaluated, let's use `posts | slice:"0:10"` but ponto2 slice returns a list of lists. Wait, we can just replace with `{% for post in posts %}` and handle truncation if needed manually, or just use `posts` if we don't care deeply about the slice here. Let's do `{% for post in posts | slice:":10" %}` or just manually edit. Let's use a simpler regex for now.
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.slice\([^)]+\)\.forEach\(([a-zA-Z0-9_]+)\)\s*\{\s*%>', r'{% for \2 in \1 %}', content)

    # `<%- include('./includes/head', { siteTitle: (typeof post !=='undefined' ? post.title : '关于' ) + ' | ' + ... }) %>`
    # Let's simplify and just include it without variables since Jinja2 shares the context anyway. EJS explicit variable passing is not always needed in Jinja2
    content = re.sub(r'<%\-\s*include\([\'"]\./includes/head[\'"],\s*\{.*?\}\)\s*%>', r'{% include "includes/head.html" %}', content)

    # Variable replacement that was missed
    content = re.sub(r'<%\s*=\s*(.*?)\s*%>', r'{{ \1 }}', content)
    content = re.sub(r'<%\-\s*(.*?)\s*%>', r'{{ \1 | safe }}', content)

    # `<% displayPosts.forEach(post, index) { %>`
    content = re.sub(r'<%\s*([a-zA-Z0-9_\.]+)\.forEach\(([a-zA-Z0-9_]+),\s*[a-zA-Z0-9_]+\)\s*\{\s*%>', r'{% for \2 in \1 %}', content)

    # The rest of <% ... %> that are logic code in head.html and blog.html
    # Some EJS files have complex JS logic. By convention we should convert the logic or simplify.
    
    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(content)

base_path = "/Volumes/Work/VibeCoding/Gridea Pro/frontend/public/default-files/themes/amore-jinja2/templates"
for root, dirs, files in os.walk(base_path):
    for file in files:
        if file.endswith('.html'):
            filepath = os.path.join(root, file)
            fix_remaining(filepath)
