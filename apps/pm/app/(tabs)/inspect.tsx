import React from 'react';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Button, CalendarIcon, color, ListRow,
  Metric, MetricRow, RowCard, Screen, space, StatusPill,
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
        <MetricRow>
          <Metric value={loading ? '…' : String(rows.length)} label="slots today" tone="blue" />
          <Metric value={loading ? '…' : String(upcoming)} label="still to run" tone={upcoming ? 'amber' : 'green'} />
        </MetricRow>

        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: 'Nothing to show today',
            body: 'Viewing slots are published on a listing, and prospects book them from the Find app. Set the times and they appear here.',
          }}
          rows={rows.map((v, i) => (
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
        />

        <Button
          label="Set viewing times"
          onPress={() => router.push('/viewings')}
          style={{ marginHorizontal: space(4), marginTop: space(3) }}
        />
      </Screen>
    </>
  );
}
