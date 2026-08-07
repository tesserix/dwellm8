import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Button, Card, Screen, ListRow, StatusPill, Metric, CalendarIcon,
  color, font, space, ErrorState,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { fmtDate, fmtTime, useOpsInspections } from '../../src/data/worklists';

/** Inspections — today's viewing slots and how they went (GET /v1/inspections). */

const stateTone: Record<string, Tone> = {
  open: 'blue', booked: 'amber', closed: 'neutral', done: 'green',
};

export default function Inspect() {
  const router = useRouter();
  const { loading, error, data: rows } = useOpsInspections();

  const upcoming = rows.filter((i) => !i.outcome && i.state !== 'closed').length;

  return (
    <>
      <AppHeader
        title="Inspections"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={s.metrics}>
          <Metric value={loading ? '…' : String(rows.length)} label="slots today" tone="blue" />
          <Metric value={loading ? '…' : String(upcoming)} label="still to run" tone={upcoming ? 'amber' : 'green'} />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View> : null}
          {error ? <ErrorState error={error} inline /> : null}
          {rows.map((v, i) => (
            <ListRow
              key={v.id ?? String(i)}
              left={<CalendarIcon size={22} c={color.accent} />}
              title={`${fmtDate(v.starts_at)}, ${fmtTime(v.starts_at)}`}
              subtitle={`${v.meeting_point ?? 'meeting point not set'}${v.assigned_to ? ` · ${v.assigned_to}` : ''}`}
              meta={`${v.duration_mins ?? 30} mins${typeof v.remaining === 'number' ? ` · ${v.remaining} places left` : ''}${v.note ? ` · ${v.note}` : ''}`}
              right={
                <StatusPill
                  text={v.outcome ? v.outcome.replace(/_/g, ' ') : (v.state ?? 'open')}
                  tone={v.outcome ? 'green' : stateTone[v.state ?? 'open'] ?? 'neutral'}
                />
              }
              onPress={v.listing_id
                ? () => router.push({ pathname: '/viewing-times', params: { id: v.listing_id! } })
                : undefined}
              last={i === rows.length - 1}
            />
          ))}
          {!loading && !error && !rows.length ? (
            <Text style={s.empty}>
              No inspection slots today. Slots are published on listings; prospects book them from
              the Find app.
            </Text>
          ) : null}
        </Card>

        <Button
          label="Set viewing times"
          onPress={() => router.push('/viewings')}
          style={{ marginHorizontal: space(4), marginTop: space(3) }}
        />
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(2) },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
});
