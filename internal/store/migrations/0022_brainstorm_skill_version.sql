-- Версия скилла шторма в задаче (задача #5027): свой скилл теперь пишется
-- как orchestrator-brainstorming@<версия>. Всё, что стартовало раньше на
-- своём скилле, — версия 1.0 (дословная копия superpowers 6.4.1).
-- Штатный superpowers:brainstorming и '' не трогаются. Идемпотентна.

UPDATE tasks SET brainstorm_skill = 'orchestrator-brainstorming@1.0'
WHERE brainstorm_skill = 'orchestrator-brainstorming';
