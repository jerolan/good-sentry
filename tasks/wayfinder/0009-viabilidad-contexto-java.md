---
id: WF-0009
title: Evaluar utilidad y esfuerzo del contexto Java
status: open
labels: [wayfinder:research]
parent: WF-0001
assignee: jerolan
claim_session: 01a17668-8461-7c71-bb8a-c64f02f890be/java_scope_check
blocked_by: [WF-0008]
---

## Question

¿Qué evidencia permite juzgar si el contexto fuente Java es un requisito
fundamental o una mejora adicional, y si incorporarlo a GoodSentry sería
una pieza pequeña aprovechando lo ya existente?
Separar captura básica de errores Java de enriquecimiento con fuentes.
Investigar flujos públicos SDK/CLI y las capacidades existentes de GoodSentry.
Identificar el cambio mínimo demostrado y las incógnitas, sin implementar,
leer código del servidor Sentry/GlitchTip/Relay ni declarar trivialidad o
popularidad sin evidencia. No decidir alcance por el usuario.

Evidencia esperada en research/java-scope-feasibility.md. Investigación
acotada delegada a java_scope_check; el primario revisa sus conclusiones.
