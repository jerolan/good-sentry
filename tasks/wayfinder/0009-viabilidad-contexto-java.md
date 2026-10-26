---
id: WF-0009
title: Evaluar utilidad y esfuerzo del contexto Java
status: closed
labels: [wayfinder:research]
parent: WF-0001
assignee: jerolan
claim_session: null
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

## Resolution comments

2026-10-25, America/Mexico_City. Investigación factual revisada por el primario.
Se verificaron los anuncios oficiales Sentry de junio de 2023 y abril de 2026,
la UI de frames, mapEvent y el guard de platform native del reprocesamiento.
El contexto Java es una feature conocida de enriquecimiento. No es requisito
para que un cliente capture una excepción con stack, ni hay evidencia de que
sea fundamental para los usuarios de GoodSentry. No se ha probado aquí captura
Java contra GoodSentry. El código ofrece UI/almacenamiento reutilizables, pero
falta un camino JVM demostrado de interpretación de bundle, asociación con el
evento y enriquecimiento. No hay base para llamarlo trivial o bug conocido.

[Informe, referencias y límites](research/java-scope-feasibility.md).
La investigación responde a la condición factual; la decisión de aplazar o
incluir permanece con el usuario en el ticket de compatibilidad.
