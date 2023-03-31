{{ define "packages" }}

    {{ with .packages}}
        <p>Packages:</p>
        <ul>
            {{ range . }}
                <li>
                    <a href="#{{- packageAnchorID . -}}">{{ packageDisplayName . }}</a>
                </li>
            {{ end }}
        </ul>
    {{ end}}

    {{ range .packages }}
        <h2 id="{{- packageAnchorID . -}}">
            {{- packageDisplayName . -}}
        </h2>

        {{ with (index .GoPackages 0 )}}
            {{ with .DocComments }}
                {{ safe (renderComments .) }}
            {{ end }}
        {{ end }}

        Resource Types:

        <ul>
            {{- range (visibleTypes (sortedTypes .Types)) -}}
                {{ if isExportedType . -}}
                    <li>
                        <a href="{{ linkForType . }}">{{ typeDisplayName . }}</a>
                    </li>
                {{- end }}
            {{- end -}}
        </ul>

        {{ range (visibleTypes (sortedTypes .Types))}}
            {{ template "type" .  }}
        {{ end }}
    {{ end }}

    <div>
        <p>This page was automatically generated with <code>gen-crd-api-reference-docs</code></p>
    </div>
{{ end }}