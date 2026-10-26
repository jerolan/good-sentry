---
id: WF-0001
title: Cerrar GoodSentry con un contrato estable
status: open
labels: [wayfinder:map]
parent: null
assignee: null
blocked_by: []
---

## Destination

Acordar un contrato finito de cierre de GoodSentry: mínimo funcional de una
referencia congelada de GlitchTip, evidencia exigible, sacrificios expresos y
una instrucción de Goal que permita llegar al mismo fin desde nuevas sesiones.

## Notes

Planificación. No implementar funcionalidades ni modificar el Goal activo.
Consultar curated:wayfinder, curated:writing-for-agents y, para decisiones
con el usuario, curated:grilling y curated:domain-modeling.

Acuerdos del usuario del 25 de octubre de 2026, America/Mexico_City:
- Equivalencia funcional, sin reproducir cada API o detalle interno de GlitchTip.
- Las integraciones anunciadas como verificadas necesitan comprobantes externos.
- Capacidades adicionales de Sentry pueden aplazarse por complejidad; cada
  sacrificio se decidirá expresamente. No se ha aprobado una lista de exclusiones.
- El cierre debe persistir entre sesiones y proveedores.

El estado actual sirve para identificar distancia y evidencia reutilizable;
no se presupone que reconstruir todo el historial sea un prerrequisito.
Verificar qué conocimiento hace falta para cada decisión.

Usar el [registro local](README.md). El mapa es un índice de decisiones;
SPEC.md, tasks/plan.md y tasks/todo.md siguen siendo los artefactos de producto.
Respetar main y las preferencias de trabajo directo del usuario: guardar
investigación en archivos disjuntos, sin ramas research/ ni PR.

## Decisions so far

- [Fijar la referencia pública de GlitchTip](0002-referencia-glitchtip.md): hay una candidata y fuentes delimitadas; su aceptación y discrepancias permanecen como decisiones.
- [Identificar la evidencia reutilizable de GoodSentry](0003-evidencia-goodsentry.md): existe una fotografía por commit y evidencia reutilizable para continuar sin reconstruir el historial completo.

- [Precisar contexto Java y archivo frío de la referencia](0008-contexto-java-archivo-frio.md): hay documentación de archivo de logs y errores; el flujo reproducible de contexto Java sigue sin precisar.

## Not yet specified

La referencia y el inventario pueden revelar ambigüedades de equivalencia
funcional que requieran preguntas adicionales. Registrar esas preguntas
cuando puedan formularse con precisión.

## Out of scope

Implementar el producto, ejecutar aceptación completa, cambiar proveedores,
credenciales o facturación, y reactivar o sustituir el Goal de otra sesión.
La paridad futura con novedades de Sentry/GlitchTip queda fuera de este mapa.
