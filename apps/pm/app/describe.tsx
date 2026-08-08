import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import {
  BackHeader, Card, Screen, Field, Button, ErrorState, Toast,
  color, font, radius, space, useBack,
} from '@dwellm8/mobile-shared';
import {
  usePropertyDescription, useUnitDescription,
  amenityVocabulary, featureVocabulary, facings, furnishings,
} from '../src/data/describe';

/**
 * What the building, or the flat, is like (#354) — the paragraph and the
 * amenities a renter reads before they are shown a rent.
 *
 * One screen, two records: opened with a unit it edits the flat, opened with a
 * property it edits the building. The form is the same shape either way.
 */

const words = (v: string) => v.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase());

function Chips({ vocabulary, held, onToggle }:
  { vocabulary: string[]; held: string[]; onToggle: (v: string) => void }) {
  return (
    <View style={s.chips}>
      {vocabulary.map((v) => {
        const on = held.includes(v);
        return (
          <Pressable
            key={v}
            accessibilityRole="button"
            accessibilityLabel={words(v)}
            accessibilityState={{ selected: on }}
            onPress={() => onToggle(v)}
            style={[s.chip, on && s.chipOn]}
          >
            <Text style={[s.chipInk, on && s.chipInkOn]}>{words(v)}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

export default function DescribeScreen() {
  const goBack = useBack('/(tabs)');
  const { id, unit } = useLocalSearchParams<{ id?: string; unit?: string }>();
  const building = usePropertyDescription(unit ? undefined : id);
  const flat = useUnitDescription(unit);
  const record = unit ? flat : building;

  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const save = async () => {
    setSaving(true);
    try {
      await record.save();
      say('Saved');
    } catch (err) {
      say((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <BackHeader
        title={unit ? 'About this flat' : 'About this property'}
        subtitle={record.name}
        onBack={goBack}
      />
      <Screen>
        {record.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {record.error ? <ErrorState error={record.error} /> : null}

        {!record.loading && !record.error ? (
          <>
            <Card>
              <Text style={s.note}>
                This is what a renter reads first. Say what somebody standing outside would
                notice — the road, the light, what is at the corner.
              </Text>
              <Field
                label={unit ? 'About this flat' : 'About this building'}
                value={record.about}
                onChange={record.setAbout}
                multiline
                autoCapitalize="sentences"
                autoCorrect
              />
            </Card>

            {unit ? (
              <>
                <Card>
                  <Text style={s.h}>How many</Text>
                  <Text style={s.note}>
                    Left blank means not recorded, which is not the same as none.
                  </Text>
                  <Field label="Bathrooms" value={flat.bathrooms} onChange={flat.setBathrooms}
                    keyboardType="numeric" />
                  <Field label="Balconies" value={flat.balconies} onChange={flat.setBalconies}
                    keyboardType="numeric" />
                  <Field label="Covered parking" value={flat.coveredParking}
                    onChange={flat.setCoveredParking} keyboardType="numeric" />
                </Card>

                <Card>
                  <Text style={s.h}>Facing</Text>
                  <Chips vocabulary={facings} held={flat.facing ? [flat.facing] : []}
                    onToggle={(v) => flat.setFacing(flat.facing === v ? '' : v)} />
                  <Text style={s.h}>Furnishing</Text>
                  <Chips vocabulary={furnishings} held={flat.furnishing ? [flat.furnishing] : []}
                    onToggle={(v) => flat.setFurnishing(flat.furnishing === v ? '' : v)} />
                </Card>

                <Card>
                  <Text style={s.h}>What is in the flat</Text>
                  <Chips vocabulary={featureVocabulary} held={flat.features} onToggle={flat.toggle} />
                </Card>
              </>
            ) : (
              <Card>
                <Text style={s.h}>What the building has</Text>
                <Text style={s.note}>
                  Only what is actually there. An amenity claimed and missing is the first
                  thing a renter reports.
                </Text>
                <Chips vocabulary={amenityVocabulary} held={building.amenities}
                  onToggle={building.toggle} />
              </Card>
            )}

            <Button label="Save the description" onPress={save} disabled={saving} />
          </>
        ) : null}
      </Screen>
      {toast ? <Toast text={toast} /> : null}
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  h: { ...font.title, color: color.ink, marginTop: space(3), marginBottom: space(2) },
  note: { ...font.small, color: color.inkSoft, lineHeight: 20, marginBottom: space(3) },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: space(2), marginBottom: space(2) },
  chip: { paddingHorizontal: space(3), paddingVertical: space(2), borderRadius: radius.pill, borderWidth: 1, borderColor: color.line },
  chipOn: { backgroundColor: color.accent, borderColor: color.accent },
  chipInk: { ...font.small, color: color.inkSoft },
  chipInkOn: { color: '#FFFFFF', fontWeight: '600' },
});
