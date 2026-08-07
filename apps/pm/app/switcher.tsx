import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import {
  CloseIcon, BuildingIcon, ShieldIcon, StatusPill,
  apiFromEnv, getActingGrant, setActingGrant,
  color, font, radius, shadow, space,
} from '@dwellm8/mobile-shared';
import type { OpsPortfolio } from '@dwellm8/mobile-shared';
import { refreshWorklists } from '../src/data/worklists';

/**
 * Portfolio switch (ADR-0005 on a screen): the firm's own books, or one
 * owner's under a live mandate. The whole app switches — every ops request
 * from here on declares the chosen grant, and permissions never blend.
 */

export default function Switcher() {
  const router = useRouter();
  const api = useMemo(() => apiFromEnv(), []);
  const [portfolios, setPortfolios] = useState<OpsPortfolio[]>([]);
  const [loading, setLoading] = useState(!!api);
  const [error, setError] = useState<string | null>(null);
  const active = getActingGrant();
  // Own books are the card at the top, not a mandate in the list (#268).
  const own = portfolios.find((p) => p.self_managed);
  const mandated = portfolios.filter((p) => !p.self_managed);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    api.opsPortfolios()
      .then((ps) => { if (alive) { setPortfolios(ps); setLoading(false); } })
      .catch((err: Error) => { if (alive) { setError(err.message); setLoading(false); } });
    return () => { alive = false; };
  }, [api]);

  const pick = (grantId: string | null) => {
    setActingGrant(grantId);
    refreshWorklists();
    router.back();
  };

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']}>
        <View style={{ padding: space(4) }}>
          <Pressable accessibilityRole="button" accessibilityLabel="Close"
            onPress={() => router.back()} hitSlop={10}>
            <CloseIcon size={26} w={2.2} />
          </Pressable>
          <Text style={s.title}>Switch portfolio</Text>
          <Text style={s.sub}>Whose books are you working on?</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ padding: space(4), gap: 14 }}>
        <Pressable style={[s.card, !active && s.cardActive]} onPress={() => pick(null)}>
          <View style={s.thumb}><ShieldIcon size={44} c="#9FB0C4" w={1.6} /></View>
          <View style={{ flex: 1 }}>
            <Text style={s.name}>{own ? 'Property you own yourself' : "Your firm's own books"}</Text>
            <Text style={s.meta}>
              {own
                ? `${own.property_count} ${own.property_count === 1 ? 'property' : 'properties'} · no mandate needed`
                : 'No mandate declared'}
            </Text>
          </View>
          {!active ? <StatusPill text="Current" tone="green" /> : null}
        </Pressable>

        {loading ? (
          <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View>
        ) : null}
        {error ? <Text style={s.note}>{error}</Text> : null}

        {mandated.map((p) => {
          const on = p.grant_id === active;
          return (
            <Pressable key={p.grant_id} style={[s.card, on && s.cardActive]} onPress={() => pick(p.grant_id)}>
              <View style={s.thumb}><BuildingIcon size={44} c="#9FB0C4" w={1.6} /></View>
              <View style={{ flex: 1 }}>
                <Text style={s.name}>{p.owner_name}</Text>
                <Text style={s.meta}>{p.permissions.length} permissions · managed since {p.since.slice(0, 10)}</Text>
              </View>
              {on ? <StatusPill text="Current" tone="green" /> : null}
            </Pressable>
          );
        })}

        {!loading && !error && mandated.length === 0 ? (
          <Text style={s.note}>
            No managed portfolios yet — onboard an owner from the Portfolio tab and their books
            appear here under your mandate.
          </Text>
        ) : null}

        <Text style={s.note}>
          Permissions do not carry across a switch: everything you see and do under a mandate is
          exactly what the owner granted, and the record says which grant you acted under.
        </Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.body, color: color.inkSoft, marginTop: 3 },
  card: {
    flexDirection: 'row', alignItems: 'center', gap: 14,
    backgroundColor: '#FFF', borderRadius: radius.lg, padding: space(4),
    borderWidth: 2, borderColor: 'transparent', ...shadow.card,
  },
  cardActive: { borderColor: color.accent },
  thumb: { width: 62, height: 62, borderRadius: radius.md, backgroundColor: '#E9EFF8', alignItems: 'center', justifyContent: 'center' },
  name: { ...font.h3, color: color.inkStrong },
  meta: { ...font.small, color: color.inkSoft, marginTop: 3 },
  note: { ...font.small, color: color.inkSoft, lineHeight: 18, marginTop: space(2) },
});
