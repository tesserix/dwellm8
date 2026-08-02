import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Screen, Card, Segmented, ListRow, StatusPill, Button,
  color, font, space,
} from '@dwellm8/mobile-shared';
import { savedSearches } from '../../src/data/mock';
import { useMyFind, useSavedSearches } from '../../src/data/source';
import { ListingCard } from '../../src/components/ListingCard';

/** Saved homes, and the searches that watch for new ones. */

export default function Saved() {
  const router = useRouter();
  const [tab, setTab] = useState('Homes');
  const { saved } = useMyFind();
  const watches = useSavedSearches();

  return (
    <>
      <AppHeader title="Saved" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <View style={{ marginTop: space(3), marginBottom: space(3) }}>
          <Segmented items={['Homes', 'Searches']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Homes' ? (
          saved.map((l) => <ListingCard key={l.id} l={l} onPress={() => router.push(`/listing?id=${l.id}`)} />)
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {watches.mode === 'live' ? (
              <>
                {watches.rows.map((w, i) => (
                  <ListRow
                    key={w.id}
                    title={w.city}
                    subtitle={[
                      w.locality,
                      w.bedrooms != null ? `${w.bedrooms} BHK` : '',
                      w.max_rent_minor ? `under ₹${Math.round(w.max_rent_minor / 100).toLocaleString('en-IN')}` : '',
                      w.alerts_enabled ? '' : 'alerts off',
                    ].filter(Boolean).join(' · ') || 'Any home in the city'}
                    right={w.new_matches ? <StatusPill text={`${w.new_matches} new`} tone="blue" /> : undefined}
                    onPress={() => { watches.seen(w.id); router.push('/(tabs)'); }}
                    last={i === watches.rows.length - 1}
                  />
                ))}
                {!watches.rows.length ? (
                  <Text style={s.body2}>No saved searches yet — save one from the search tab.</Text>
                ) : null}
              </>
            ) : (
              savedSearches.map((s2, i) => (
                <ListRow
                  key={s2.id}
                  title={s2.name}
                  subtitle={s2.filter}
                  right={s2.newCount ? <StatusPill text={`${s2.newCount} new`} tone="blue" /> : undefined}
                  onPress={() => router.push('/(tabs)')}
                  last={i === savedSearches.length - 1}
                />
              ))
            )}
          </Card>
        )}

        <Card>
          <Text style={s.h}>How alerts work</Text>
          <Text style={s.body}>
            A saved search tells you the moment something matches — once, for that home. No daily
            digest, and nothing when a listing is merely re-promoted.
          </Text>
          <Button label="Search again" tone="secondary" onPress={() => router.push('/(tabs)')} style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  body2: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
});
