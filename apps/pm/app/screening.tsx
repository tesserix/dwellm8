import React, { useState } from 'react';
import { useRouter } from 'expo-router';
import {
  Avatar, BackHeader, ChipRow, ListRow, Metric, MetricRow, RowCard,
  Screen, StatusPill, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { fmtDate } from '../src/data/worklists';
import { useApplications } from '../src/data/screening';

/** Screening queue — applications on the properties this firm manages (#259). */

const stateTone: Record<string, Tone> = {
  submitted: 'blue', under_review: 'amber', accepted: 'green',
  declined: 'neutral', withdrawn: 'neutral',
};

const OPEN = ['submitted', 'under_review'];

export default function Screening() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [filter, setFilter] = useState('Open');
  const { loading, error, data: rows } = useApplications();

  const list = rows.filter((a) => (filter === 'Open' ? OPEN.includes(a.state) : !OPEN.includes(a.state)));
  const open = rows.filter((a) => OPEN.includes(a.state)).length;
  const accepted = rows.filter((a) => a.state === 'accepted').length;

  return (
    <>
      <BackHeader title="Screening" subtitle="Applications on your properties" onBack={goBack} />
      <Screen>
        <MetricRow>
          <Metric value={loading ? '…' : String(open)} label="to screen" tone="blue" />
          <Metric value={loading ? '…' : String(accepted)} label="accepted" tone="green" />
        </MetricRow>

        <ChipRow items={[{ label: 'Open' }, { label: 'Settled' }]} value={filter} onChange={setFilter} />

        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: filter === 'Open' ? 'Nothing to screen' : 'Nothing settled yet',
            body: filter === 'Open'
              ? 'Applications arrive from the Find app once a listing is live. Each one lands here with its documents.'
              : 'Applications you have accepted or turned down are kept here, with the reason.',
          }}
          rows={list.map((a, i) => (
            <ListRow
              key={a.id}
              left={<Avatar initials={(a.headline ?? 'AP').slice(0, 2).toUpperCase()} tone={stateTone[a.state] ?? 'neutral'} />}
              title={a.headline ?? 'Application'}
              subtitle={`Move-in ${fmtDate(a.move_in)} · ${a.term_months} months`}
              meta={`Applied ${fmtDate(a.created_at)}`}
              right={<StatusPill text={a.state.replace(/_/g, ' ')} tone={stateTone[a.state] ?? 'neutral'} />}
              onPress={() => router.push({ pathname: '/pack', params: { id: a.id } })}
              last={i === list.length - 1}
            />
          ))}
        />
      </Screen>
    </>
  );
}
