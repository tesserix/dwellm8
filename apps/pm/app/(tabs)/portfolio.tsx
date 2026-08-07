import React, { useCallback, useMemo, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useFocusEffect, useRouter, type Href } from 'expo-router';
import {
  AppHeader, AvatarButton, Button, Card, Screen, SearchBar, ListRow, Metric,
  BuildingIcon, PlusIcon,
  color, font, space, ErrorState,
} from '@dwellm8/mobile-shared';
import { usePortfolio } from '../../src/data/portfolio';

/**
 * Portfolio — the properties under this scope (GET /v1/ops/properties): the
 * firm's own, or one owner's under the mandate the switcher picked.
 * Per-unit tenancy state lives on Collections, where the money question is.
 */

export default function Portfolio() {
  const router = useRouter();
  const [q, setQ] = useState('');
  const { loading, error, rows, units, reload } = usePortfolio();

  // A property onboarded one screen away is here when the manager comes back,
  // rather than after a relaunch (#289).
  useFocusEffect(useCallback(() => { reload(); }, [reload]));

  const list = useMemo(() => {
    const needle = q.toLowerCase();
    return rows.filter(
      (p) => !q
        || p.name.toLowerCase().includes(needle)
        || p.locality.toLowerCase().includes(needle)
        || p.city.toLowerCase().includes(needle),
    );
  }, [rows, q]);

  return (
    <>
      <AppHeader
        title="Portfolio"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={s.metrics}>
          <Metric
            value={loading ? '…' : String(rows.length)}
            label={rows.length === 1 ? 'property' : 'properties'}
            tone="blue"
          />
          <Metric
            value={loading ? '…' : String(units)}
            label={units === 1 ? 'unit managed' : 'units managed'}
            tone="green"
          />
        </View>

        <Button
          label="Onboard a new owner"
          icon={<PlusIcon size={19} c="#FFF" />}
          onPress={() => router.push('/onboard' as Href)}
          style={{ marginHorizontal: space(4), marginBottom: space(3) }}
        />

        <SearchBar value={q} onChange={setQ} placeholder="Property, locality or city" />

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? (
            <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View>
          ) : null}
          {error ? <ErrorState error={error} inline /> : null}
          {list.map((p, i) => (
            <ListRow
              key={p.id}
              left={<BuildingIcon size={22} c={color.accent} />}
              title={p.name}
              subtitle={`${p.locality ? `${p.locality}, ` : ''}${p.city}`}
              meta={`${p.kind} · ${p.unit_count} unit${p.unit_count === 1 ? '' : 's'} · ${p.code}`}
              onPress={() => router.push(`/property?id=${p.id}` as Href)}
              last={i === list.length - 1}
            />
          ))}
          {!loading && !error && !list.length ? (
            <Text style={s.empty}>
              Nothing under this scope yet — onboard an owner, or switch portfolios from the Today
              screen's title.
            </Text>
          ) : null}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  // One rhythm down the screen: every block is space(3) from the next, which
  // is the gap Card and SearchBar already carry.
  metrics: {
    flexDirection: 'row', gap: 10,
    marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3),
  },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
});
