import { DenseDataTable, DenseTableHeader, DenseTableHeadRow, DenseTableHead, DenseTableBody, DenseTableRow, DenseTableCell, DenseTag, denseTableNumCell } from '@bifrost/ui'
import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import { PatrolDispatchLog } from './PatrolDispatchLog'
import { localizePatrolLog, type PatrolOutputLanguage } from './logLanguage'
import { patrolRunLogText } from './runLog'
import { resultTag, formatDuration } from './presentation'

export function RunHistoryPanel({ skill, history, isLoading, logRunId, setLogRunId, lang, setOutputLanguage, now }: { skill: PatrolSkill; history: PatrolRun[]; isLoading: boolean; logRunId: string | null; setLogRunId: (id: string | null) => void; lang: PatrolOutputLanguage; setOutputLanguage: (lang: PatrolOutputLanguage) => void; now: number }) {
 return (
                      <div className="flex flex-col gap-1.5 py-1">
                        <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground">
                          Run history · {skill.name}
                        </span>
                        {isLoading ? (
                          <p className="text-[var(--text-dense-meta)] text-muted-foreground">Loading runs…</p>
                        ) : history.length === 0 ? (
                          <p className="text-[var(--text-dense-meta)] text-muted-foreground">No runs recorded yet.</p>
                        ) : (
                          <>
                          <DenseDataTable>
                            <DenseTableHeader>
                              <DenseTableHeadRow>
                                <DenseTableHead>Started</DenseTableHead>
                                <DenseTableHead>Trigger</DenseTableHead>
                                <DenseTableHead>Result</DenseTableHead>
                                <DenseTableHead className={denseTableNumCell}>Duration</DenseTableHead>
                                <DenseTableHead>Log</DenseTableHead>
                              </DenseTableHeadRow>
                            </DenseTableHeader>
                            <DenseTableBody>
                              {history.map(run => {
                                const preview = localizePatrolLog(patrolRunLogText(run), lang)
                                const selected = logRunId === run.id
                                return (
                                  <DenseTableRow key={run.id}>
                                    <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                                      {new Date(run.started_at).toLocaleString()}
                                    </DenseTableCell>
                                    <DenseTableCell>
                                      <DenseTag variant={run.trigger === 'cron' ? 'info' : 'neutral'}>
                                        {run.trigger}
                                      </DenseTag>
                                    </DenseTableCell>
                                    <DenseTableCell>{resultTag(run.result)}</DenseTableCell>
                                    <DenseTableCell className={denseTableNumCell}>
                                      {run.result === 'running'
                                        ? formatDuration(Math.max(0, now - Date.parse(run.started_at)))
                                        : formatDuration(run.duration_ms)}
                                    </DenseTableCell>
                                    <DenseTableCell>
                                      <button
                                        type="button"
                                        className="max-w-[28rem] truncate text-left text-[var(--text-dense-caption)] text-primary hover:underline"
                                        title={preview || 'No log'}
                                        onClick={() => setLogRunId(selected ? null : run.id)}
                                      >
                                        {preview !== '' ? preview : '—'}
                                      </button>
                                    </DenseTableCell>
                                  </DenseTableRow>
                                )
                              })}
                            </DenseTableBody>
                          </DenseDataTable>
                          {(() => {
                            const selected = history.find(r => r.id === logRunId) ?? history[0]
                            if (selected == null) {
                              return (
                                <p className="text-[var(--text-dense-meta)] text-muted-foreground">
                                  No dispatch log yet. L0 local probes write steps here while running; Cursor Cloud runs usually only store the final report.
                                </p>
                              )
                            }
                            return (
                              <PatrolDispatchLog
                                run={selected}
                                skill={skill}
                                lang={lang}
                                onLangChange={setOutputLanguage}
                                emptyHint="No dispatch log yet. L0 local probes write steps here while running; Cursor Cloud runs usually only store the final report."
                              />
                            )
                          })()}
                          </>
                        )}
                      </div>
 )
}
