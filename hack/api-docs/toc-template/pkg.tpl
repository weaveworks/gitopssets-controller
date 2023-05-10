{{ define "packages" }}
    {{ range .packages }}
        [
        {{ range $index, $element := (visibleTypes (sortedTypes .Types))}}
            {{if $index}},{{end}}
            {{ template "type" $element }}
        {{ end }}
        ]
    {{ end }}
{{ end }}