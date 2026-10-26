# Registro local de decisiones

Este registro usa Markdown porque no hay un tracker configurado para Wayfinder.
El mapa de entrada es [Cerrar GoodSentry con un contrato estable](0001-cierre-goodsentry.md).
Para configurar otro tracker, usar `/setup-matt-pocock-skills`; migrar este
registro conservando identidades, resoluciones y relaciones.

## Operaciones de Wayfinding

Cada archivo numerado es una issue. Su frontmatter contiene identidad, título,
estado, etiquetas, padre, responsable y dependencias. Los únicos labels son
`wayfinder:map`, `wayfinder:research` y `wayfinder:grilling`.
`blocked_by` contiene identidades locales; este tracker no tiene bloqueo nativo.

Al retomar, cargar el mapa y consultar solo hijos abiertos cuyas dependencias
estén cerradas y cuyo assignee esté vacío. Elegir primero por id ascendente.
Reclamar antes de trabajar estableciendo `assignee: jerolan` y el identificador
de sesión en `claim_session`. Una reclamación vigente excluye otras sesiones.
Una sesión abandonada requiere verificar su inactividad antes de liberar el claim.

Resolver como research o grilling según la etiqueta. En grilling, la respuesta
la da el usuario. Registrar la respuesta como comentario bajo
`## Resolution comments`, con fecha, evidencia y dudas restantes.
Luego cerrar la issue, vaciar claim_session y agregar al mapa un enlace de una
línea a la decisión. El detalle permanece únicamente en su issue.

La planificación cierra cuando no quedan hijos abiertos ni incertidumbres
necesarias en Not yet specified. Resolver como máximo un ticket por sesión,
excepto investigaciones. Registrar nuevas preguntas como nuevos hijos y
conectar dependencias después de asignarles identidad.

Los informes en research/ son evidencia, no decisiones de alcance.
SPEC.md sigue siendo la autoridad de diseño; plan.md y todo.md conservan
orden y ejecución. Este mapa no sustituye esos documentos ni autoriza construir.
Al terminar el mapa se entregará un contrato propuesto para aprobación, antes
de integrarlo en los documentos canónicos o iniciar el nuevo Goal.
