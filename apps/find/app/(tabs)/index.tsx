import React, { useMemo, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Screen, SearchBar, ChipRow, Card, Segmented,
  StatusPill, Button, MapPinIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { useSearch } from '../../src/data/source';
import { ListingCard } from '../../src/components/ListingCard';

/**
 * Search.
 *
 * Ordering is the whole product. Promoted listings sit above the fold and say
 * so; everything under them is ranked by how recently the lister replied to an
 * enquiry, because a portal full of unanswered listings is worthless.
 */

export default function Search() {
  const router = useRouter();
  const [q, setQ] = useState('');
  const [bhk, setBhk] = useState('Any');
  const [sort, setSort] = useState('Relevant');
  const { loading, listings, error } = useSearch();

  const list = useMemo(() => {
    let out = listings.filter(
      (l) =>
        !q ||
        l.locality.toLowerCase().includes(q.toLowerCase()) ||
        l.city.toLowerCase().includes(q.toLowerCase()) ||
        l.title.toLowerCase().includes(q.toLowerCase()),
    );
    if (bhk !== 'Any') out = out.filter((l) => `${l.bhk} BHK` === bhk);
    if (sort === 'Newest') out = out.slice().sort((a, b) => b.daysLeft - a.daysLeft);
    else if (sort === 'Rent') out = out.slice().sort((a, b) => a.rentPaise - b.rentPaise);
    else out = out.slice().sort((a, b) => Number(b.boosted) - Number(a.boosted));
    return out;
  }, [q, bhk, sort, listings]);

  return (
    <>
      <AppHeader
        title="Bengaluru and Pune"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={{ marginTop: space(3) }}>
          <SearchBar value={q} onChange={setQ} placeholder="Locality, building or city" />
        </View>

        <ChipRow
          items={[{ label: 'Any' }, { label: '1 BHK' }, { label: '2 BHK' }, { label: '3 BHK' }]}
          value={bhk}
          onChange={setBhk}
        />

        <View style={{ marginBottom: space(3) }}>
          <Segmented items={['Relevant', 'Newest', 'Rent']} value={sort} onChange={setSort} />
        </View>

        <View style={s.countRow}>
          <MapPinIcon size={16} c={color.inkSoft} />
          <Text style={s.count}>{list.length} homes to rent</Text>
          <View style={{ flex: 1 }} />
          <Text style={s.countSub}>Verified listings only</Text>
        </View>

        {loading ? (
          <View style={{ paddingVertical: space(8), alignItems: 'center' }}>
            <ActivityIndicator />
          </View>
        ) : null}
        {error ? (
          <Card>
            <Text style={s.emptyTitle}>Could not reach Dwellm8</Text>
            <Text style={s.emptyBody}>{error}</Text>
          </Card>
        ) : null}

        {list.map((l) => (
          <ListingCard key={l.id} l={l} onPress={() => router.push(`/listing?id=${l.id}`)} />
        ))}

        {!list.length && !loading && !error ? (
          <Card>
            <Text style={s.emptyTitle}>Nothing here yet</Text>
            <Text style={s.emptyBody}>
              Save this search and we will tell you the day something matches — no daily digest,
              only the ones that fit.
            </Text>
            <Button label="Save this search" tone="secondary" onPress={() => router.push('/(tabs)/saved')} style={{ marginTop: space(4) }} />
          </Card>
        ) : null}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text="Why these are different" tone="green" />
          </View>
          <Text style={s.body}>
            Every listing here has had its ownership document, its address and its lister's identity
            checked before it went live. A listing runs for 90 days and then comes down — so what you
            are reading is either current or gone, never a flat that was let last winter.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  countRow: { flexDirection: 'row', alignItems: 'center', gap: 7, paddingHorizontal: space(4), marginBottom: space(3) },
  count: { ...font.label, color: color.inkStrong },
  countSub: { ...font.small, color: color.inkSoft },
  emptyTitle: { ...font.h3, color: color.inkStrong },
  emptyBody: { ...font.body, color: color.inkSoft, marginTop: 8, lineHeight: 21 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(3), lineHeight: 21 },
});
