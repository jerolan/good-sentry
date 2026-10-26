# Referencia pública candidata de GlitchTip

Investigación de Fijar la referencia pública de GlitchTip. Consulta:
25 de octubre de 2026, America/Mexico_City. Informe factual y propuesta;
no constituye aceptación de alcance ni selección definitiva de versión.

## Candidata congelable y sus límites

Los [tags oficiales](https://gitlab.com/glitchtip/glitchtip-backend/-/tags)
muestran v6.2.6, fechado el 7 de agosto de 2026, como primer tag visible.
Se verificó únicamente su identidad mediante git ls-remote, sin checkout ni
lectura del servidor: d3039088e913a591a8e6544a2d2fffc865a9711a.
Esto permite proponer v6.2.6 como referencia de producto. La lista consultada
no sustituye una garantía de que nunca haya otro canal o tag posterior.

El [anuncio de 6.2](https://glitchtip.com/blog/2026-06-22-glitchtip-6-2-released/)
fija capacidades de la familia publicadas el 22 de junio de 2026; no enumera
todos los cambios de sus parches. El
[anuncio de 6.1](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/)
explica capacidades heredadas como logs, MCP y minidump.
Las guías de producto son documentación rolling, no documentación
garantizada del tag propuesto. La equivalencia deberá congelarse como lista
explícita con fuentes y dudas, no como obligación perpetua de seguir URLs.

Se consultó el [OpenAPI público](https://app.glitchtip.com/api/openapi.json)
sin leer implementación:
- 272138 bytes; 115 paths.
- SHA256: 7b60511c4823a0dfe2b82fe1cdb16e5c5d092c250bb5b9ac87a8e842df948360.
- info.version es 1.0.0. Es la versión declarada del contrato; no prueba que
  el servidor sea GlitchTip 1.0.0 ni que corresponda al tag v6.2.6.
- La existencia de un endpoint no prueba todas sus reglas ni su funcionamiento.

## Matriz inicial de capacidades observables

Esta matriz agrupa la superficie para elaborar criterios con el usuario.
No es todavía un inventario exhaustivo ni una aceptación automática.

| Área candidata | Resultado observable que sugieren las fuentes | Fuente primaria y límite |
|---|---|---|
| Errores y DSN | Crear proyecto, configurar un SDK con su DSN, capturar un fallo y verlo como issue con detalle | [Error tracking](https://glitchtip.com/documentation/error-tracking/). Deben fijarse clientes y versiones; la guía no promete cada SDK existente. |
| Alertas | Una regla por frecuencia genera email a miembros del equipo o entrega por webhook | [Error tracking](https://glitchtip.com/documentation/error-tracking/). Los destinos reales y sus recibos requieren aceptación propia. |
| Source maps | Inyectar Debug IDs, subir mapas y obtener stacks legibles | [Error tracking](https://glitchtip.com/documentation/error-tracking/). Usa glitchtip-cli beta; no implica equivalencia total con cualquier CLI Sentry. |
| Rendimiento | Consultar grupos de transacciones, estadísticas de latencia/cantidad/errores y desglose de spans; observar tendencias y consultas N+1 donde aplique | [Performance](https://glitchtip.com/documentation/performance/). El resultado puede equivalerse sin replicar DuckDB internamente. |
| Logs | Ingerir logs de SDK, buscar por severidad/servicio/entorno/texto y navegar a la traza cuando hay trace_id | [Logs](https://glitchtip.com/documentation/logs/). Raw OTLP tiene contradicción documental descrita abajo. |
| Uptime | HEAD/GET/POST y heartbeat producen estados y avisos conforme a respuestas y plazos | [Uptime](https://glitchtip.com/documentation/uptime-monitoring/). La guía no define cada detalle temporal ni prueba TCP/SSL; esos casos requieren fuente adicional si se incluyen. |
| Diagnóstico nativo | Ingerir minidumps y mejorar stacktraces; source contexts Java aparece como capacidad de 6.1 | [Release 6.1](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/). No enumera todas las arquitecturas, formatos o grados de unwind que GoodSentry debería soportar. |
| MCP | Habilitar endpoint, autenticar por OAuth o token y consultar/actualizar información mediante 17 herramientas documentadas | [MCP](https://glitchtip.com/documentation/mcp/). Incluye organizaciones/proyectos, issues, rendimiento, alertas/monitores y logs. Debe fijarse un cliente y la aceptación de sus flujos. |
| Integraciones de lectura | GitLab Error Tracking y Grafana Sentry datasource consumen datos mediante token | [Integrations](https://glitchtip.com/documentation/integrations/). La declaración general de compatibilidad Sentry es una aspiración amplia; los dos flujos documentados son candidatos concretos. |
| Identidad y administración | Configurar acceso, organizaciones y proveedores sociales/OIDC | [Install](https://glitchtip.com/documentation/install/) y [OpenAPI público](https://app.glitchtip.com/api/openapi.json). La guía enumera DigitalOcean, Gitea, GitHub, GitLab, Google, Microsoft, NextCloud, Okta y OIDC; falta acordar cómo demostrar equivalencia con esa superficie. |
| Operación y retención | Despliegue con PostgreSQL, limpieza configurable y consulta de datos archivados en almacenamiento local o S3 | [Install](https://glitchtip.com/documentation/install/). PostgreSQL es requerido y Valkey opcional. No exige copiar Django, Valkey ni DuckDB en Go. |

## Contradicciones que impiden convertir documentación en cierre automático

1. Raw OTLP: el [anuncio 6.2](https://glitchtip.com/blog/2026-06-22-glitchtip-6-2-released/)
   declara ingestión nativa de logs por /v1/logs sin Sentry SDK. La
   [guía de logs](https://glitchtip.com/documentation/logs/) dice que no hay
   receptor OTLP HTTP directo y exige envolver mediante SDK. Las dos fuentes
   son oficiales. Falta decidir precedencia y, si afecta al mínimo, comprobar
   el flujo externamente contra una instancia de la versión congelada.
2. Correo: el anuncio 6.2 permite omitirlo y oculta funciones dependientes;
   la guía de instalación conserva instrucciones de correo entre requisitos.
   No derivar un bloqueo de arranque sin probar qué exige la referencia.
3. Beta frente a contrato estable: la guía de errores propone glitchtip-cli
   beta. El cierre debe elegir comandos y versiones concretos, en lugar de
   exigir cualquier evolución de una herramienta beta.
4. Configuración no equivale a promesa funcional: la guía de instalación
   incluye opciones de infraestructura y operación. Falta separar resultados
   que el usuario necesita de tecnologías que GoodSentry no debe reproducir.

## Preguntas que la evidencia deja abiertas

- ¿Se acepta v6.2.6 como referencia de producto, con una lista de capacidades
  aprobada que congele la interpretación de sus documentos rolling?
- ¿Qué combinaciones SDK/CLI/plataforma definen el uso esperado y cuáles
  requieren cobertura adicional? Las fuentes no permiten inferir todas.
- ¿Cuál es la aceptación nativa mínima: crash real simbolicado en una
  plataforma representativa o una matriz más amplia?
- ¿Qué integraciones sociales y destinos de alertas se verificarán uno a uno,
  y qué equivalencia representativa, si alguna, acepta el usuario?
- ¿Cómo se resuelven las contradicciones documentales antes de declarar un
  requisito obligatorio o sacrificarlo?
- ¿Qué capacidades opcionales de GlitchTip forman parte del mínimo funcional?
  Que una función sea opcional de activar no prueba que esté fuera del mínimo.
- ¿Cómo se trata un servicio externo sin acceso disponible? La decisión
  previa exige recibos externos para anunciar una integración como verificada;
  este informe no autoriza sustituirlos por simulaciones.

## Propuesta de uso de esta investigación

Usar versión/tag e identidad exacta como referencia candidata, y convertir
las capacidades en criterios funcionales con identificadores y evidencia
propia. Mantener incertidumbres explícitas hasta decidirlas. Las funciones
futuras mencionadas como próximas en anuncios no son capacidades entregadas.
Una infraestructura diferente o un SDK no probado tampoco constituye por sí
solo equivalencia o incumplimiento.

No se descargó ni consultó código de implementación del servidor Sentry,
Relay o GlitchTip, sus pruebas, esquema de base de datos ni assets.
Se leyeron páginas oficiales, metadatos de tags y contrato OpenAPI público.
No se construyó, ejecutó ni modificó GoodSentry; solo se escribe este informe.
