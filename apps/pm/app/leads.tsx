import React, { useState } from 'react';
import { useRouter } from 'expo-router';
import {
  Avatar, BackHeader, ChipRow, ListRow, Metric, MetricRow, RowCard,
  Screen, StatusPill, useBack,
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
        <MetricRow>
          <Metric value={loading ? '…' : String(open)} label="open enquiries" tone="blue" />
          <Metric value={loading ? '…' : String(converted)} label="converted" tone="green" />
        </MetricRow>

        <ChipRow items={[{ label: 'Open' }, { label: 'Settled' }]} value={filter} onChange={setFilter} />

        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: filter === 'Open' ? 'No enquiries yet' : 'Nothing settled yet',
            body: filter === 'Open'
              ? 'Enquiries arrive from the Find app when somebody answers a live listing. Advertise a vacant unit and they land here.'
              : 'Enquiries you have answered or let go are kept here, so nothing is lost.',
          }}
          rows={list.map((e, i) => (
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
        />
      </Screen>
    </>
  );
}
