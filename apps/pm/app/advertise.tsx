import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, color, Field, font, Screen, SectionTitle, space,
  SwitchRow, Toast, useBack,
} from '@dwellm8/mobile-shared';
import { useUnitRecord } from '../src/data/unit';
import { useAdvertise } from '../src/data/advertise';

/**
 * Putting a vacant flat on the market (#370).
 *
 * The API has refused an under-stated listing since #142: every cost is given
 * or explicitly nil before it can be published. This screen asks for them in
 * the order a renter reads them, and shows the API's refusal as its own words.
 */
export default function Advertise() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { unit: unitId } = useLocalSearchParams<{ unit?: string }>();
  const { loading, error, unit, property } = useUnitRecord(unitId);
  const advert = useAdvertise();

  const [headline, setHeadline] = useState('');
  const [rent, setRent] = useState('');
  const [deposit, setDeposit] = useState('');
  const [maintenance, setMaintenance] = useState('');
  const [parking, setParking] = useState('');
  const [otherMonthly, setOtherMonthly] = useState('');
  const [oneTime, setOneTime] = useState('');
  const [bedrooms, setBedrooms] = useState('');
  const [from, setFrom] = useState('');
  const [stateCode, setStateCode] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    if (property?.state_code) setStateCode(property.state_code);
  }, [property?.state_code]);

  const publish = async () => {
    if (!confirmed) {
      setToast('Tick that these are all the costs — a renter is quoted what is here.');
      return;
    }
    if (!property || !unit) return;
    await advert.publish({
      property_id: property.id,
      unit_id: unit.id,
      headline: headline.trim(),
      locality: property.locality,
      city: property.city,
      state_code: stateCode.trim().toUpperCase(),
      rent_minor: paise(rent),
      deposit_minor: paise(deposit),
      maintenance_minor: paise(maintenance),
      parking_minor: paise(parking),
      other_monthly_minor: paise(otherMonthly),
      one_time_minor: paise(oneTime),
      costs_confirmed: true,
      bedrooms: bedrooms ? Number(bedrooms) : undefined,
      carpet_area_sqft: unit.carpet_area_sqft,
      available_from: from.trim() || undefined,
    });
  };

  useEffect(() => {
    if (advert.live) router.back();
  }, [advert.live, router]);

  return (
    <>
      <BackHeader
        title="Advertise this flat"
        subtitle={unit ? `Flat ${unit.code}` : undefined}
        onBack={goBack}
      />
      <Screen>
        {loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {error ? <Card><Text style={s.note}>{error}</Text></Card> : null}

        {property ? (
          <Card>
            <Text style={s.where}>{property.name}</Text>
            <Text style={s.note}>
              {[property.locality, property.city].filter(Boolean).join(', ')} — a listing shows
              the locality, never the address.
            </Text>
          </Card>
        ) : null}

        {unit ? (
          <>
            <SectionTitle>What is being let</SectionTitle>
            <Card>
              <Field label="Headline" value={headline} onChange={setHeadline}
                placeholder="Two bedrooms off the park" autoCapitalize="sentences" />
              <Field label="Bedrooms" value={bedrooms} onChange={setBedrooms}
                keyboardType="numeric" placeholder="2" />
              <Field label="Available from" value={from} onChange={setFrom}
                placeholder="YYYY-MM-DD" />
              <Field label="State" value={stateCode} onChange={setStateCode}
                placeholder="KA" autoCapitalize="characters" />
            </Card>

            <SectionTitle>What it costs</SectionTitle>
            <Card>
              <Field label="Rent a month" value={rent} onChange={setRent}
                keyboardType="numeric" placeholder="32000" />
              <Field label="Deposit" value={deposit} onChange={setDeposit}
                keyboardType="numeric" placeholder="96000" />
              <Field label="Maintenance a month" value={maintenance} onChange={setMaintenance}
                keyboardType="numeric" placeholder="Leave empty if there is none" />
              <Field label="Parking a month" value={parking} onChange={setParking}
                keyboardType="numeric" placeholder="Leave empty if there is none" />
              <Field label="Anything else a month" value={otherMonthly} onChange={setOtherMonthly}
                keyboardType="numeric" placeholder="Leave empty if there is none" />
              <Field label="One-off charges" value={oneTime} onChange={setOneTime}
                keyboardType="numeric" placeholder="Leave empty if there are none" />
              <SwitchRow
                label="These are all the costs"
                hint="Empty means none, not unknown. A cost added later is one a renter was not quoted."
                value={confirmed}
                onChange={setConfirmed}
                last
              />
            </Card>

            {advert.error ? (
              <Card>
                <Text style={s.refusal}>{advert.error}</Text>
                <Text style={s.note}>
                  Nothing was advertised. Change what the refusal names and put it up again.
                </Text>
              </Card>
            ) : null}

            <Button label="Put it on the market" onPress={publish} disabled={advert.working} />
          </>
        ) : null}
      </Screen>
      {/* Outside the scroll: a manager who has scrolled to the button is who
          this refusal is for, and inside it the toast lands off screen. */}
      {toast ? <Toast text={toast} /> : null}
    </>
  );
}

// A rupee figure typed by a manager, in the minor units everything stores.
const paise = (v: string) => Math.round(Number(v || 0) * 100);

const s = StyleSheet.create({
  waiting: { paddingVertical: space(6), alignItems: 'center' },
  where: { ...font.h3, color: color.inkStrong },
  note: { ...font.small, color: color.inkSoft, marginTop: space(2), lineHeight: 19 },
  refusal: { ...font.body, color: color.negative, lineHeight: 21 },
});
