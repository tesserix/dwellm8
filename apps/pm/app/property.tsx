import React from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, ListRow, Metric,
  HouseArt, color, font, inr, space, type OpsUnit, useBack,
} from '@dwellm8/mobile-shared';
import { usePropertyRecord } from '../src/data/property';
import { useOwnershipEvidence } from '../src/data/ownership';

/** One property and every unit in it — the record a manager opens on site. */
export default function PropertyScreen() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, property, units, occupied, vacant, spokenFor, rentRollPaise } =
    usePropertyRecord(id);
  const ownership = useOwnershipEvidence(id);

  return (
    <>
      <BackHeader
        title={property?.name ?? 'Property'}
        subtitle={property ? [property.locality, property.city].filter(Boolean).join(', ') : undefined}
        onBack={goBack}
      />
      <Screen>
        {loading ? (
          <View style={s.waiting}><ActivityIndicator /></View>
        ) : null}
        {error ? <Card><Text style={s.empty}>{error}</Text></Card> : null}

        {property ? (
          <>
            <Card padded={false} style={{ overflow: 'hidden' }}>
              <View style={s.art}><HouseArt size={150} /></View>
              <View style={{ padding: space(4) }}>
                <Text style={s.title}>{property.name}</Text>
                <Text style={s.sub}>{property.address_line1}</Text>
                <Text style={s.rent}>{inr(rentRollPaise, { noPaise: true })} per month let</Text>
                <KeyValue k="Reference" v={property.code} />
                <KeyValue k="Kind" v={property.kind} last />
              </View>
            </Card>

            {/* A flat let on somebody's say-so has no answer when the real
                owner appears, so the record says what is on file (#339). */}
            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              <ListRow
                title="Ownership"
                subtitle={ownership.proven
                  ? 'The deed, or the power of attorney for it, is on file'
                  : 'The deed or a power of attorney is still wanted'}
                right={<StatusPill
                  text={ownership.proven ? 'Proven' : 'Wanted'}
                  tone={ownership.proven ? 'green' : 'amber'}
                />}
                onPress={() => router.push(`/deeds?id=${property.id}`)}
              />
              {/* What the firm may and may not do here, in writing (#340). */}
              <ListRow
                title="Management agreement"
                subtitle={ownership.held.includes('management_agreement')
                  ? 'A signed copy is on file'
                  : 'Print it, sign on paper, file the signed copy'}
                right={<StatusPill
                  text={ownership.held.includes('management_agreement') ? 'Signed' : 'To print'}
                  tone={ownership.held.includes('management_agreement') ? 'green' : 'neutral'}
                />}
                onPress={() => router.push(`/agreement?id=${property.id}`)}
                last
              />
            </Card>

            <View style={s.metrics}>
              <Metric value={String(occupied)} label="let" tone="green" />
              <Metric value={String(vacant)} label="empty" tone="amber" />
              {spokenFor ? <Metric value={String(spokenFor)} label="taken" tone="amber" /> : null}
            </View>

            <Card padded={false} style={{ paddingHorizontal: space(4) }}>
              <Text style={[s.h, { marginTop: space(4) }]}>Units</Text>
              {units.map((u, i) => (
                <ListRow
                  key={u.id}
                  title={u.code}
                  subtitle={stateOf(u)}
                  meta={metaFor(u)}
                  right={<StatusPill text={pillFor(u)} tone={u.lease_id ? 'green' : 'amber'} />}
                  onPress={() => router.push(`/unit?id=${u.id}`)}
                  last={i === units.length - 1}
                />
              ))}
              {!units.length ? (
                <Text style={s.empty}>No units on this property yet.</Text>
              ) : null}
            </Card>
          </>
        ) : null}
      </Screen>
    </>
  );
}

// A unit whose tenancy starts later is empty today and still not on offer, so
// it reads as taken from a date rather than as free (#304).
function pillFor(u: OpsUnit): string {
  if (u.let_from) return 'From ' + u.let_from;
  return u.lease_id ? 'Let' : 'Empty';
}

// Nobody lives here yet, but the flat is not on offer either — and the list is
// where it would be offered from (#314).
function stateOf(u: OpsUnit): string {
  if (u.tenant) return u.tenant;
  return u.let_from ? 'Taken, not yet moved in' : 'Vacant';
}

function metaFor(u: OpsUnit): string {
  // The pill already carries the date; twice on one row reads as two dates.
  if (u.let_from) {
    return `${inr(u.rent_amount_minor ?? 0, { noPaise: true })} · ${u.kind}`;
  }
  if (u.lease_id) {
    return `${inr(u.rent_amount_minor ?? 0, { noPaise: true })} · to ${u.lease_ends ?? '—'}`;
  }
  return u.kind;
}

const s = StyleSheet.create({
  art: { backgroundColor: '#EAF1FB', alignItems: 'center', paddingVertical: space(4) },
  title: { ...font.h2, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  rent: { ...font.h3, color: color.accent, marginTop: space(3), marginBottom: space(2) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  metrics: { flexDirection: 'row', gap: 10, marginBottom: space(3) },
  waiting: { paddingVertical: space(6), alignItems: 'center' },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
});
