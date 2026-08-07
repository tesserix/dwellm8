import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ChipRow, ListRow, Avatar, StatusPill, Metric,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { fmtDate, useOpsEnquiries } from '../src/data/worklists';

/** Leads — the discovery funnel's enquiries, org side (GET /v1/enquiries). */

const stateTone: Record<string, Tone> = {
  new: 'blue', responded: 'violet', visit_scheduled: 'amber', visited: 'amber',
  converted: 'green', closed: 'neutral', cancelled: 'neutral',
};

export default function Leads() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [filter, setFilter] = useState('Open');
  const { loading, error, data: rows } = useOpsEnquiries();

  const settled = (st: string) => st === 'converted' || st === 'closed' || st === 'cancelled';
  const list = rows.filter((e) => (filter === 'Open' ? !settled(e.state) : settled(e.state)));
  const open = rows.filter((e) => !settled(e.state)).length;
  const converted = rows.filter((e) => e.state === 'converted').length;

  return (
    <>
      <BackHeader title="Leads" subtitle="Enquiries on your listings" onBack={goBack} />
      <Screen>
        <View style={s.metrics}>
          <Metric value={loading ? '…' : String(open)} label="open enquiries" tone="blue" />
          <Metric value={loading ? '…' : String(converted)} label="converted" tone="green" />
        </View>

        <ChipRow items={[{ label: 'Open' }, { label: 'Settled' }]} value={filter} onChange={setFilter} />

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View> : null}
          {error ? <Text style={s.empty}>{error}</Text> : null}
          {list.map((e, i) => (
            <ListRow
              key={e.id}
              left={<Avatar initials={(e.headline ?? e.kind).slice(0, 2).toUpperCase()} tone={stateTone[e.state] ?? 'neutral'} />}
              title={e.headline ?? e.kind}
              subtitle={e.message ?? e.contact_masked ?? '—'}
              meta={`${e.kind} · ${fmtDate(e.created_at)}${e.scheduled_for ? ` · visit ${fmtDate(e.scheduled_for)}` : ''}`}
              right={<StatusPill text={e.state.replace(/_/g, ' ')} tone={stateTone[e.state] ?? 'neutral'} />}
              onPress={() => {}}
              last={i === list.length - 1}
            />
          ))}
          {!loading && !error && !list.length ? (
            <Text style={s.empty}>
              {filter === 'Open' ? 'No open enquiries — they arrive from the Find app.' : 'Nothing settled yet.'}
            </Text>
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
