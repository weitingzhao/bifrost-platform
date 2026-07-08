import { TaskControlCenter, type TaskControlCenterProps } from '@/components/task-mode/TaskControlCenter'

/** Task Control Center page — thin wrapper for ConsolePage routing. */
export function TaskControlCenterPage(props: TaskControlCenterProps) {
  return <TaskControlCenter {...props} />
}
