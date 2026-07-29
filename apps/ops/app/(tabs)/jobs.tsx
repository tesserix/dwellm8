import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, ChipRow, ListRow, StatusPill, Metric,
  Button, SearchBar, WrenchIcon, PlusIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { tickets, vendors } from '../../src/data/mock';

/**
 * The ticket queue and dispatch board.
 *
 * Sorted so that anything with an SLA clock at risk sits above anything
 * comfortable, whatever its age.
 */

const statusTone: Record<string, Tone> = {
  New: 'blue',
  Triaged: 'violet',
  Quoted: 'amber',
  Scheduled: 'blue',
  'In progress': 'amber',
  'Awaiting owner': 'violet',
  Resolved: 'green',
};

const priorityTone: Record<string, Tone> = { Emergency: 'red', Urgent: 'amber', Routine: 'neutral' };

export default function Jobs() {
  const router = useRouter();
  const [filter, setFilter] = useState('Open');
  const [q, setQ] = useState('');

  const list = useMemo(() => {
    const base = tickets.filter(
      (t) => !q || t.title.toLowerCase().includes(q.toLowerCase()) || t.unit.toLowerCase().includes(q.toLowerCase()),
    );
    const view =
      filter === 'Open' ? base.filter((t) => t.status !== 'Resolved')
      : filter === 'Breaching' ? base.filter((t) => t.slaLeft.includes('Breach') || t.slaLeft.includes('left') && t.priority === 'Emergency')
      : filter === 'Awaiting owner' ? base.filter((t) => t.status === 'Awaiting owner')
      : base;
    const rank = (p: string) => (p === 'Emergency' ? 0 : p === 'Urgent' ? 1 : 2);
    return view.slice().sort((a, b) => rank(a.priority) - rank(b.priority));
  }, [filter, q]);

  const breaching = tickets.filter((t) => t.slaLeft.includes('Breach')).length;
  const unassigned = tickets.filter((t) => !t.vendor && t.status !== 'Resolved').length;

  return (
    <>
      <AppHeader
        title="Job queue"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={s.metrics}>
          <Metric value={String(tickets.filter((t) => t.status !== 'Resolved').length)} label="open jobs" tone="blue" />
          <Metric value={String(breaching)} label="SLA breached" tone="red" />
          <Metric value={String(unassigned)} label="no vendor yet" tone="amber" />
        </View>

        <ChipRow
          items={[{ label: 'Open' }, { label: 'Breaching' }, { label: 'Awaiting owner' }, { label: 'All' }]}
          value={filter}
          onChange={setFilter}
        />

        <SearchBar value={q} onChange={setQ} placeholder="Job or unit" />

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {list.map((t, i) => (
            <ListRow
              key={t.id}
              title={t.title}
              subtitle={`${t.unit} · ${t.tenant}`}
              meta={`${t.category} · raised ${t.raised}${t.vendor ? ` · ${t.vendor}` : ' · unassigned'}`}
              right={
                <View style={{ alignItems: 'flex-end', gap: 6 }}>
                  <StatusPill text={t.status} tone={statusTone[t.status]} />
                  <Text style={[s.sla, t.slaLeft.includes('Breach') && { color: color.negative, fontWeight: '800' }]}>
                    {t.slaLeft}
                  </Text>
                </View>
              }
              left={
                <View style={{ alignItems: 'center', gap: 4, width: 34 }}>
                  <WrenchIcon size={20} c={t.priority === 'Emergency' ? color.negative : color.accent} />
                  <StatusPill text={t.priority[0]} tone={priorityTone[t.priority]} />
                </View>
              }
              onPress={() => router.push(`/ticket?id=${t.id}`)}
              last={i === list.length - 1}
              tone={t.slaLeft.includes('Breach') ? 'red' : undefined}
            />
          ))}
          {!list.length ? <Text style={s.empty}>No jobs match this filter.</Text> : null}
        </Card>

        <Button
          label="Log a new job"
          icon={<PlusIcon size={19} c="#FFF" />}
          onPress={() => router.push('/ticket?id=new')}
          style={{ marginHorizontal: space(4), marginBottom: space(4) }}
        />

        <Card>
          <Text style={s.helpTitle}>Panel vendors</Text>
          {vendors.slice(0, 4).map((v, i) => (
            <ListRow
              key={v.id}
              title={v.name}
              subtitle={v.trade}
              meta={`★ ${v.rating} · ${v.jobs} jobs · responds in ~${v.responseMins}m${
                v.ratePaise ? ` · callout ${inr(v.ratePaise, { noPaise: true })}` : ' · under AMC'
              }`}
              right={<StatusPill text={v.onPanel ? 'On panel' : 'Off panel'} tone={v.onPanel ? 'green' : 'neutral'} />}
              onPress={() => router.push('/dispatch?ticket=t-2188')}
              last={i === 3}
            />
          ))}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4) },
  sla: { ...font.small, color: color.inkSoft },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
  helpTitle: { ...font.h3, color: color.inkStrong, marginBottom: space(2) },
});
