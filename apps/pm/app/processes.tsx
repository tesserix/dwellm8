import React from 'react';
import { useRouter } from 'expo-router';
import {
  BackHeader, ClipboardIcon, color, count, ListRow, Metric,
  MetricRow, RowCard, Screen, StatusPill, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { useOpsChecklists } from '../src/data/worklists';

/** Processes — ADR-0032's checklists across the organisation, late first
 * (GET /v1/checklists). */

const stateTone: Record<string, Tone> = {
  open: 'blue', completed: 'green', abandoned: 'neutral',
};

const processLabel = (p: string) => p.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase());

export default function Processes() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { loading, error, data: rows } = useOpsChecklists();

  const open = rows.filter((c) => c.state === 'open');
  const late = open.filter((c) => (c.days_overdue ?? 0) > 0);

  return (
    <>
      <BackHeader title="Processes" subtitle="Move-ins, move-outs, onboardings" onBack={goBack} />
      <Screen>
        <MetricRow>
          <Metric value={loading ? '…' : String(open.length)} label="under way" tone="blue" />
          <Metric value={loading ? '…' : String(late.length)} label="running late" tone={late.length ? 'red' : 'green'} />
        </MetricRow>

        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: 'No processes running',
            body: 'A move-in or a move-out opens a checklist here, with every step and who it waits on.',
          }}
          rows={rows.map((c, i) => (
            <ListRow
              key={c.id}
              left={<ClipboardIcon size={22} c={(c.days_overdue ?? 0) > 0 ? color.negative : color.accent} />}
              title={processLabel(c.process)}
              subtitle={`${c.settled}/${c.tasks} steps settled · ${c.blocking_outstanding} blocking outstanding`}
              meta={`${c.next_due_on ? `next due ${c.next_due_on}` : 'nothing due'}${(c.days_overdue ?? 0) > 0 ? ` · ${count(c.days_overdue ?? 0, 'day')} overdue` : ''}`}
              right={<StatusPill text={c.state} tone={stateTone[c.state] ?? 'neutral'} />}
              onPress={() => router.push(`/checklist?id=${c.id}`)}
              last={i === rows.length - 1}
            />
          ))}
        />
      </Screen>
    </>
  );
}
