{{ define "type" }}
    { "level": 3, "value": "{{ .Name.Name }}", "id": "{{ anchorIDForType . }}" }
{{ end }}
