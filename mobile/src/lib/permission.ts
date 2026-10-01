// Permission prompts in chat mode: the daemon reads a Claude Code permission
// dialog off the tmux pane and publishes it as pending_quiz with
// source="permission" (docs/13-chat.md). One option = one keypress.
import { ApiError } from '../api/client'
import type { PendingQuiz, PermissionEcho } from '../api/types'

/** option_indices value that presses Escape instead of a digit. */
export const ESC_INDEX = -1

export const UNCONFIRMED_TEXT = 'Ответ не подтвердился'

export function isPermissionQuiz(q: PendingQuiz | undefined): boolean {
  return q?.source === 'permission'
}

/** The daemon sends the question as "<Title>\n\n<Context>". */
export function splitPermissionQuestion(question: string): { title: string; context: string } {
  const at = question.indexOf('\n\n')
  if (at === -1) return { title: question.trim(), context: '' }
  return { title: question.slice(0, at).trim(), context: question.slice(at + 2).replace(/\s+$/, '') }
}

export function resolvedPermissionLine(p: PermissionEcho): string {
  const answer = p.answered_via === 'terminal' ? 'отвечено в терминале' : p.answer_label || 'отвечено'
  return `Разрешение: ${p.title} → ${answer}`
}

export function permissionErrorText(e: unknown): string {
  if (e instanceof ApiError && e.code === 'prompt_changed') return 'Диалог изменился, обновляю…'
  return (e as Error).message
}
