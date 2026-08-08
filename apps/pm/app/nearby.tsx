import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, Button, Segmented, ErrorState, Toast, StatusPill,
  color, font, radius, space, useBack,
} from '@dwellm8/mobile-shared';
import { useNearby } from '../src/data/nearby';

/**
 * What is near this property (#354).
 *
 * The distance is the one the manager measured along the road, not a line
 * drawn between two geocodes — a renter walks the road.
 */

const kinds: { key: string; one: string; many: string }[] = [
  { key: 'school', one: 'School', many: 'Schools' },
  { key: 'college', one: 'College', many: 'Colleges' },
  { key: 'childcare', one: 'Creche', many: 'Creches' },
  { key: 'hospital', one: 'Hospital', many: 'Hospitals' },
  { key: 'clinic', one: 'Clinic', many: 'Clinics' },
  { key: 'pharmacy', one: 'Pharmacy', many: 'Pharmacies' },
  { key: 'metro', one: 'Metro', many: 'Metro stations' },
  { key: 'bus', one: 'Bus stop', many: 'Bus stops' },
  { key: 'railway', one: 'Railway', many: 'Railway stations' },
  { key: 'airport', one: 'Airport', many: 'Airports' },
  { key: 'market', one: 'Market', many: 'Markets' },
  { key: 'supermarket', one: 'Supermarket', many: 'Supermarkets' },
  { key: 'mall', one: 'Mall', many: 'Malls' },
  { key: 'park', one: 'Park', many: 'Parks' },
  { key: 'gym', one: 'Gym', many: 'Gyms' },
  { key: 'bank', one: 'Bank', many: 'Banks' },
  { key: 'place_of_worship', one: 'Place of worship', many: 'Places of worship' },
  { key: 'other', one: 'Other', many: 'Other places' },
];

const heading = (key: string) => kinds.find((k) => k.key === key)?.many ?? key;

const modes = ['walk', 'drive', 'transit'];

export default function NearbyScreen() {
  const goBack = useBack('/(tabs)');
  const { id, name } = useLocalSearchParams<{ id?: string; name?: string }>();
  const nearby = useNearby(id);

  const [kind, setKind] = useState('school');
  const [place, setPlace] = useState('');
  const [metres, setMetres] = useState('');
  const [mode, setMode] = useState('walk');
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const add = async () => {
    setSaving(true);
    try {
      await nearby.add({
        category: kind,
        name: place,
        distance_m: Number(metres.trim()) || 0,
        travel_mode: mode as 'walk' | 'drive' | 'transit',
      });
      setPlace('');
      setMetres('');
      say('Added to what is nearby');
    } catch (err) {
      say((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async (placeId: string, label: string) => {
    try {
      await nearby.remove(placeId);
      say(`${label} is off the list`);
    } catch (err) {
      say((err as Error).message);
    }
  };

  return (
    <>
      <BackHeader title="What's nearby" subtitle={name} onBack={goBack} />
      <Screen>
        {nearby.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {nearby.error ? <ErrorState error={nearby.error} onRetry={nearby.reload} /> : null}

        {nearby.groups.map((g) => (
          <Card key={g.category} padded={false} style={{ paddingHorizontal: space(4) }}>
            <Text style={s.h}>{heading(g.category)}</Text>
            {g.places.map((p) => (
              <View key={p.id} style={s.row}>
                <View style={{ flex: 1 }}>
                  <Text style={s.name}>{p.name}</Text>
                  <Text style={s.away}>{p.away}</Text>
                  {p.tags.length ? (
                    <View style={s.tags}>
                      {p.tags.map((t) => (
                        <StatusPill key={t} text={t.replace(/_/g, ' ')} tone="neutral" />
                      ))}
                    </View>
                  ) : null}
                </View>
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel={`Remove ${p.name}`}
                  onPress={() => remove(p.id, p.name)}
                  style={s.remove}
                >
                  <Text style={s.removeInk}>Remove</Text>
                </Pressable>
              </View>
            ))}
          </Card>
        ))}

        {!nearby.loading && !nearby.error && !nearby.places.length ? (
          <Card>
            <Text style={s.empty}>
              Nothing recorded yet. A renter asks which school and how far before they ask the
              rent — add the ones you would name on a viewing.
            </Text>
          </Card>
        ) : null}

        <Card>
          <Text style={s.h}>Add a place</Text>
          <View style={s.kinds}>
            {kinds.map((k) => (
              <Pressable
                key={k.key}
                accessibilityRole="button"
                accessibilityState={{ selected: kind === k.key }}
                onPress={() => setKind(k.key)}
                style={[s.chip, kind === k.key && s.chipOn]}
              >
                <Text style={[s.chipInk, kind === k.key && s.chipInkOn]}>{k.many}</Text>
              </Pressable>
            ))}
          </View>
          <Field label="Name of the place" value={place} onChange={setPlace}
            placeholder="Spotswood Primary School" autoCapitalize="words" />
          <Field label="How far, in metres" value={metres} onChange={setMetres}
            keyboardType="numeric" placeholder="600" />
          <Text style={s.label}>Getting there</Text>
          <Segmented items={modes} value={mode} onChange={setMode} />
          <Button label="Add to the list" onPress={add} disabled={saving}
            style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
      {toast ? <Toast text={toast} /> : null}
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  h: { ...font.title, color: color.ink, marginTop: space(4), marginBottom: space(2) },
  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: space(3), borderTopWidth: 1, borderTopColor: color.line },
  name: { ...font.body, color: color.ink, fontWeight: '600' },
  away: { ...font.small, color: color.inkSoft, marginTop: 2 },
  tags: { flexDirection: 'row', flexWrap: 'wrap', gap: space(2), marginTop: space(2) },
  remove: { paddingHorizontal: space(3), paddingVertical: space(2) },
  removeInk: { ...font.small, color: color.negative, fontWeight: '600' },
  empty: { ...font.small, color: color.inkSoft, lineHeight: 20 },
  kinds: { flexDirection: 'row', flexWrap: 'wrap', gap: space(2), marginBottom: space(4) },
  chip: { paddingHorizontal: space(3), paddingVertical: space(2), borderRadius: radius.pill, borderWidth: 1, borderColor: color.line },
  chipOn: { backgroundColor: color.accent, borderColor: color.accent },
  chipInk: { ...font.small, color: color.inkSoft },
  chipInkOn: { color: '#FFFFFF', fontWeight: '600' },
  label: { ...font.small, color: color.inkSoft, marginBottom: space(2) },
});
