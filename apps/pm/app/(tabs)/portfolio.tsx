import React, { useCallback, useMemo, useState } from 'react';
import { StyleSheet } from 'react-native';
import { useFocusEffect, useRouter, type Href } from 'expo-router';
import {
  AppHeader, AvatarButton, BuildingIcon, Button, color, font, ListRow,
  Metric, MetricRow, PlusIcon, RowCard, Screen, SearchBar, space,
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
        <MetricRow>
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
        </MetricRow>

        <Button
          label="Onboard a new owner"
          icon={<PlusIcon size={19} c="#FFF" />}
          onPress={() => router.push('/onboard' as Href)}
          style={{ marginHorizontal: space(4), marginBottom: space(3) }}
        />

        <SearchBar value={q} onChange={setQ} placeholder="Property, locality or city" />

        <RowCard
          loading={loading}
          error={error}
          empty={{
            title: 'Nothing under this scope',
            body: "Onboard an owner and their building, or switch portfolios from the Today screen's title.",
          }}
          rows={list.map((p, i) => (
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
        />
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  // One rhythm down the screen: every block is space(3) from the next, which
  // is the gap Card and SearchBar already carry.
});
