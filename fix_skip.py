with open('internal/source/parserdef/sqlite.go', 'r') as f:
    content = f.read()

content = content.replace("""		// Skip empty rows (per plan §6.9: warn + skip, no silent swallow).
		// Skip empty rows (per plan §6.9: warn + skip, no silent swallow).
		if strings.TrimSpace(content) == "" {""", """		// Skip empty rows (per plan §6.9: warn + skip, no silent swallow).
		if strings.TrimSpace(content) == "" {""")

with open('internal/source/parserdef/sqlite.go', 'w') as f:
    f.write(content)
