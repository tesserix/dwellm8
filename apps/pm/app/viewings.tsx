import React from 'react';
import { useRouter } from 'expo-router';
import {
  BackHeader, CalendarIcon, color, ListRow, RowCard, Screen, StatusPill, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { useOpsListings } from '../src/data/viewings';

/** The firm's listings, as the way in to their viewing times (#333). */

const stateTone: Record<string, Tone> = {
  draft: 'neutral', live: 'green', paused: 'amber', suspended: 'amber',
  let: 'blue', withdrawn: 'neutral',
};

export default function Viewings() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { loading, error, data: listings } = useOpsListings();

  return (
    <>
      <BackHeader title="Viewing times" subtitle="Set when people can come and see" onBack={goBack} />
      <Screen>
        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: 'No listings yet',
            body: 'Advertise a vacant unit first. Viewing times are set on the listing people are answering.',
          }}
          rows={listings.map((l, i) => (
            <ListRow
              key={l.id}
              left={<CalendarIcon size={22} c={color.accent} />}
              title={l.headline || 'Untitled listing'}
              subtitle={[l.locality, l.city].filter(Boolean).join(', ')}
              meta={l.bedrooms ? `${l.bedrooms} bed` : undefined}
              right={<StatusPill text={l.state} tone={stateTone[l.state] ?? 'neutral'} />}
              onPress={() => router.push({ pathname: '/viewing-times', params: { id: l.id, headline: l.headline } })}
              last={i === listings.length - 1}
            />
          ))}
        />
      </Screen>
    </>
  );
}
