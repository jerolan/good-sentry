---
id: WF-0005
title: Decidir qué capacidades de Sentry se aplazan
status: closed
labels: [wayfinder:grilling]
parent: WF-0001
assignee: jerolan
claim_session: null
blocked_by: [WF-0004]
---

## Question

¿Qué capacidades adicionales de Sentry pueden aplazarse para cerrar GoodSentry
y qué capacidades complejas siguen siendo obligatorias por pertenecer al
mínimo funcional acordado?

Para cada propuesta, mostrar utilidad, complejidad o bloqueo demostrado,
consecuencia observable y alternativa. Conservar lo ya implementado.
El usuario aprueba exclusiones individualmente; complejidad por sí sola
no autoriza sacrificar el mínimo de GlitchTip.

## Inputs from the compatibility decision

Consultar [los flujos aprobados](0004-contrato-compatibilidad.md) antes de
proponer sacrificios. Esa issue es dueña del aplazamiento Java y del alcance
de la prueba nativa real. No duplicar ni revertir esos acuerdos por inferencia.
Reconciliar las brechas históricas contra el alcance y la evidencia vigentes;
una casilla abierta no demuestra por sí sola funcionalidad ausente. Conservar
las capacidades ya implementadas y los controles existentes.

## Working discussion

2026-10-25, America/Mexico_City. Sesión 01a175c3-902d-7420-acd0-fc155e823ca5.
Se retoma tras el cierre de los flujos. Propuestas pendientes de aprobación:

1. Aplazar conexión automática con repositorios y descubrimiento remoto de
   commits. Conservar releases, metadatos locales de commits y deploys ya
   aprobados. Consecuencia: el usuario o su CI suministra esos metadatos.
   Evidencia: docs/compatibility.md, Current gaps; SPEC.md, Public provenance,
   contratos públicos de releases. La complejidad no está medida.
2. Aplazar WebAuthn como segundo factor. Conservar TOTP, códigos de recuperación
   y controles existentes, incluido require2FA en trabajo activo. Consecuencia:
   no exigir llaves físicas o passkeys como segundo factor para este cierre.
   Evidencia: docs/compatibility.md, Current gaps; SPEC.md, Two-factor
   authentication. La complejidad no está medida.
3. Aplazar throttling global de eventos por organización. Conservar muestreo
   por proyecto y cuotas por clave existentes. Consecuencia: el cierre no
   exige una política común de muestreo de eventos para todos los proyectos
   de una organización. Evidencia: docs/compatibility.md, Available capabilities and
   limits; SPEC.md, Compatible project update and deletion. La complejidad
   no está medida.

Las propuestas son decisiones de límite de alcance, no bloqueos técnicos
probados. No se afirma que estén ausentes en GlitchTip. Si una contradice el
mínimo congelado, su aprobación debe registrarse como excepción expresa.
No se propone eliminar código ni debilitar controles. No se aplazan por
inferencia políticas adicionales de issues, recibos externos, archivo frío
ni otros flujos aprobados. Fuentes Java y prueba nativa mantienen su dueño
en la decisión de flujos. No se cierra esta issue sin respuesta del usuario.

## Resolution comments

2026-10-25, America/Mexico_City. El usuario respondió "Si" a la ronda que
solicitaba aprobar individualmente los tres aplazamientos propuestos.
Quedan aprobados para este cierre la conexión automática con repositorios,
WebAuthn como segundo factor y el muestreo global de eventos por organización.
Se registran como excepciones expresas al contrato de cierre donde afecten
al mínimo congelado de GlitchTip, sin afirmar que GlitchTip carezca de ellas.

Se mantienen releases, metadatos locales de commits, deploys, TOTP, códigos
de recuperación, require2FA y controles existentes, muestreo por proyecto
y cuotas por clave, con el alcance descrito en la discusión. No se elimina
código. No se extiende la aprobación a políticas de issues no identificadas,
recibos externos, archivo frío ni otros flujos aprobados. La complejidad de
los candidatos no fue medida; la aprobación fija un límite de alcance.

La decisión queda cerrada. Fuentes Java y aceptación nativa conservan sus
acuerdos en la decisión de flujos. Las pruebas externas y el contrato final
siguen pendientes en sus decisiones respectivas. No se modifica el producto,
los documentos canónicos de implementación ni ningún Goal en esta resolución.
