import { describe, expect, it } from 'vitest'
import { graphOf, parsePipelineYaml, validatePipeline } from './pipelineYaml'

// The plan's own example, verbatim.
const FIX_CI = `name: fix-ci
workspace: styr
timeout: 2h
steps:
  - id: triage
    template: "CI failure triage"
    with: { alert: "{{ .payload.title }}" }
  - id: fix
    needs: [triage]
    template: "Apply fix"
    with: { plan: "{{ .steps.triage.report.proposed_action }}" }
    worktree: own
    retries: 1
  - id: verify
    needs: [fix]
    template: "Run tests and report"
    worktree: shared
  - id: review-each
    needs: [triage]
    foreach: "{{ .steps.triage.report.files }}"
    template: "Review file"
`

describe('parsePipelineYaml', () => {
  it('reads the plan example into four steps', () => {
    const parsed = parsePipelineYaml(FIX_CI)
    expect(parsed.name).toBe('fix-ci')
    expect(parsed.workspace).toBe('styr')
    expect(parsed.timeout).toBe('2h')
    expect(parsed.steps.map((s) => s.id)).toEqual(['triage', 'fix', 'verify', 'review-each'])
  })

  it('keeps the shape of each step', () => {
    const [triage, fix, verify, review] = parsePipelineYaml(FIX_CI).steps
    expect(triage.template).toBe('CI failure triage')
    expect(fix.needs).toEqual(['triage'])
    expect(fix.retries).toBe(1)
    expect(verify.worktree).toBe('shared')
    expect(review.foreach).toBe('{{ .steps.triage.report.files }}')
    expect(review.worktree).toBe('own')
  })

  it('builds one edge per dependency', () => {
    const graph = graphOf(parsePipelineYaml(FIX_CI))
    expect(graph.nodes).toHaveLength(4)
    expect(graph.edges).toEqual([
      { from: 'triage', to: 'fix' },
      { from: 'fix', to: 'verify' },
      { from: 'triage', to: 'review-each' },
    ])
  })
})

describe('validatePipeline', () => {
  it('accepts the plan example', () => {
    const result = validatePipeline(FIX_CI)
    expect(result.errors).toEqual([])
    expect(result.ok).toBe(true)
  })

  it('reports a dependency on a step defined below, with its line', () => {
    const result = validatePipeline(`name: broken
workspace: styr
steps:
  - id: triage
    template: "CI failure triage"
  - id: fix
    needs: [verify]
    template: "Apply fix"
  - id: verify
    needs: [fix]
    template: "Run tests and report"
`)
    expect(result.ok).toBe(false)
    expect(result.errors).toHaveLength(1)
    expect(result.errors[0]).toEqual({
      line: 7,
      message: 'Step "fix" needs "verify", which is not defined above it.',
    })
    // The graph still comes back so the preview keeps its shape while the
    // definition is being fixed.
    expect(result.graph.nodes).toHaveLength(3)
  })

  it('reports duplicate ids, a missing template and too many retries', () => {
    const result = validatePipeline(`name: dupes
workspace: styr
steps:
  - id: one
    template: "A"
  - id: one
    retries: 9
`)
    const messages = result.errors.map((e) => e.message)
    expect(messages).toContain('Two steps are called "one"; ids have to be unique.')
    expect(messages).toContain('Step "one" does not say which template to run.')
    expect(messages).toContain('Step "one" asks for 9 retries; 3 is the most a step can have.')
  })

  it('needs a name, a workspace and at least one step', () => {
    const result = validatePipeline('steps:\n')
    expect(result.errors.map((e) => e.message)).toEqual([
      'A pipeline needs a name.',
      'A pipeline needs a workspace.',
      'A pipeline needs at least one step.',
    ])
  })
})
