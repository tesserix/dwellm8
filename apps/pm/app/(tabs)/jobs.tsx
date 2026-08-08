import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, ChipRow, color, font, inr, ListRow,
  Metric, MetricRow, RowCard, Screen, SearchBar, StatusPill, WrenchIcon,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { TICKET_STATUS_LABEL, fmtDate, useOpsTickets } from '../../src/data/worklists';

/**
 * The ticket queue — the firm's side of #237. Tenants raise most of these
 * from the Live app; everything a manager does lands back on the tenant's
 * timeline.
 */

const statusTone: Record<string, Tone> = {
  open: 'blue',
  acknowledged: 'violet',
  scheduled: 'blue',
  in_progress: 'amber',
  resolved: 'green',
  cancelled: 'neutral',
};

export default function Jobs() {
  const router = useRouter();
  const [filter, setFilter] = useState('Open');
  const [q, setQ] = useState('');
  const { loading, error, data: tickets } = useOpsTickets(filter === 'Settled');

  const list = useMemo(() => {
    const needle = q.toLowerCase();
    const base = tickets.filter(
      (t) => !q
        || t.title.toLowerCase().includes(needle)
        || (t.unit ?? '').toLowerCase().includes(needle)
        || (t.property ?? '').toLowerCase().includes(needle),
    );
    if (filter === 'Unscheduled') return base.filter((t) => t.status === 'open' || t.status === 'acknowledged');
    return base;
  }, [tickets, filter, q]);

  const open = filter === 'Settled' ? 0 : tickets.length;
  const unscheduled = tickets.filter((t) => t.status === 'open' || t.status === 'acknowledged').length;
  const unassessed = tickets.filter((t) => !t.liability && t.status !== 'resolved' && t.status !== 'cancelled').length;

  return (
    <>
      <AppHeader
        title="Job queue"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <MetricRow>
          <Metric value={loading ? '…' : String(open)} label="open jobs" tone="blue" />
          <Metric value={loading ? '…' : String(unscheduled)} label="not yet scheduled" tone={unscheduled ? 'amber' : 'green'} />
          <Metric value={loading ? '…' : String(unassessed)} label="liability unassessed" tone={unassessed ? 'amber' : 'green'} />
        </MetricRow>

        <ChipRow
          items={[{ label: 'Open' }, { label: 'Unscheduled' }, { label: 'Settled' }]}
          value={filter}
          onChange={setFilter}
        />

        <SearchBar value={q} onChange={setQ} placeholder="Job, unit or property" />

        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: filter === 'Settled' ? 'Nothing settled yet' : 'Nothing to fix',
            body: filter === 'Settled'
              ? 'Jobs appear here once they are resolved or cancelled, with what they cost and who bore it.'
              : 'Tenants raise repairs from the Live app. Anything they report lands here for you to schedule.',
            art: <WrenchIcon size={34} c={color.accent} />,
          }}
          rows={list.map((t, i) => (
            <ListRow
              key={t.ticket_id}
              title={t.title}
              subtitle={`${t.unit ?? '—'}, ${t.property ?? ''}`}
              meta={`${t.category} · raised ${fmtDate(t.raised_at)}${t.vendor ? ` · ${t.vendor}` : ''}${t.cost_minor ? ` · ${inr(t.cost_minor)}` : ''}`}
              right={
                <View style={{ alignItems: 'flex-end', gap: 6 }}>
                  <StatusPill text={TICKET_STATUS_LABEL[t.status] ?? t.status} tone={statusTone[t.status] ?? 'neutral'} />
                  {!t.liability && t.status !== 'resolved' && t.status !== 'cancelled'
                    ? <Text style={s.needs}>needs assessment</Text>
                    : null}
                </View>
              }
              left={
                <View style={{ alignItems: 'center', width: 34 }}>
                  <WrenchIcon size={20} c={color.accent} />
                </View>
              }
              onPress={() => router.push(`/ticket?id=${t.ticket_id}`)}
              last={i === list.length - 1}
            />
          ))}
        />
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  needs: { ...font.small, color: '#B4541B' },
});
