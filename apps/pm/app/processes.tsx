import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Metric, ClipboardIcon,
  color, count, font, space,
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
  const { loading, error, data: rows } = useOpsChecklists();

  const open = rows.filter((c) => c.state === 'open');
  const late = open.filter((c) => (c.days_overdue ?? 0) > 0);

  return (
    <>
      <BackHeader title="Processes" subtitle="Move-ins, move-outs, onboardings" onBack={() => router.back()} />
      <Screen>
        <View style={s.metrics}>
          <Metric value={loading ? '…' : String(open.length)} label="under way" tone="blue" />
          <Metric value={loading ? '…' : String(late.length)} label="running late" tone={late.length ? 'red' : 'green'} />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View> : null}
          {error ? <Text style={s.empty}>{error}</Text> : null}
          {rows.map((c, i) => (
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
          {!loading && !error && !rows.length ? (
            <Text style={s.empty}>No processes yet — a move-in or move-out fires one.</Text>
          ) : null}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(2) },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
});
