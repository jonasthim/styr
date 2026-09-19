// Starting a pipeline by hand. The only thing to decide is the input the
// steps render their variables from, so the dialog is that one JSON object
// and nothing else - checked before the button will do anything, because an
// unattended chain of agents is the worst place to find a typo.
import { useEffect, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import type { Pipeline, PipelineStartResult } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { Button, Dialog, DialogContent, Field, Textarea } from '../ui'

export function StartPipelineDialog({
  pipeline,
  open,
  onOpenChange,
}: {
  pipeline: Pipeline | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [input, setInput] = useState('{}')
  const [starting, setStarting] = useState(false)

  useEffect(() => {
    if (open) setInput('{}')
  }, [open, pipeline])

  let inputError = ''
  let parsed: Record<string, unknown> = {}
  if (input.trim()) {
    try {
      const value: unknown = JSON.parse(input)
      if (!value || typeof value !== 'object' || Array.isArray(value)) {
        inputError = 'Input must be a JSON object.'
      } else {
        parsed = value as Record<string, unknown>
      }
    } catch {
      inputError = 'This is not valid JSON.'
    }
  }

  async function handleStart() {
    if (!pipeline || inputError) return
    setStarting(true)
    try {
      const result = await api<PipelineStartResult>(`/api/v1/pipelines/${pipeline.id}/start`, {
        method: 'POST',
        json: { input: parsed },
      })
      void queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
      onOpenChange(false)
      void navigate({ to: '/pipeline-runs/$id', params: { id: result.pipeline_run_id } })
    } catch (err) {
      toast({
        title: 'Could not start the pipeline',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setStarting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={`Start ${pipeline?.name ?? 'pipeline'}`}
        description="Every step renders its variables from this input, the way a delivery's payload would."
        width={520}
        footer={
          <>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button variant="primary" loading={starting} disabled={!!inputError} onClick={() => void handleStart()}>
              Start pipeline
            </Button>
          </>
        }
      >
        <Field label="Input" labelAside="optional" hint="A JSON object, its keys are available to every step's templates at the top level, for example {{ .title }}." error={inputError}>
          {({ id, 'aria-describedby': describedBy, 'aria-invalid': invalid }) => (
            <Textarea
              id={id}
              mono
              rows={8}
              aria-describedby={describedBy}
              aria-invalid={invalid}
              value={input}
              onChange={(e) => setInput(e.target.value)}
            />
          )}
        </Field>
      </DialogContent>
    </Dialog>
  )
}
