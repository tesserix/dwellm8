import React from 'react';
import { View, Text, StyleSheet, Image, Pressable } from 'react-native';
import {
  StatusPill, ShieldIcon, CheckCircleIcon, color, font, inr, radius, shadow, space,
} from '@dwellm8/mobile-shared';
import { photos } from '../data/mock';
import type { Listing } from '../data/mock';

/**
 * One listing in a scroll.
 *
 * Three things decide whether a seeker taps: the rent, whether the photograph
 * looks like the actual flat, and whether anyone has checked that the lister
 * owns it. All three are on the card, and the verification badge is the only
 * thing a boost cannot buy.
 */

export function ListingCard({ l, onPress }: { l: Listing; onPress: () => void }) {
  const verified = l.verification.ownership && l.verification.address && l.verification.identity;

  return (
    <Pressable style={s.card} onPress={onPress} accessibilityRole="button" accessibilityLabel={l.title}>
      <View style={s.imgWrap}>
        <Image source={l.photoUrl ? { uri: l.photoUrl } : photos[l.photo]} style={s.img} resizeMode="cover" />
        <View style={s.badges}>
          {l.boosted ? <StatusPill text="Promoted" tone="violet" /> : null}
          {l.managed ? <StatusPill text="Managed by Dwellm8" tone="green" /> : null}
        </View>
        <View style={s.rentTag}>
          <Text style={s.rent}>{inr(l.rentPaise, { noPaise: true })}</Text>
          <Text style={s.rentSub}>per month</Text>
        </View>
      </View>

      <View style={{ padding: space(4) }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7 }}>
          {verified ? <CheckCircleIcon size={17} c={color.positive} /> : <ShieldIcon size={17} c={color.inkFaint} />}
          <Text style={[s.verify, !verified && { color: color.inkSoft }]}>
            {verified ? 'Ownership verified' : 'Verification in progress'}
          </Text>
          <View style={{ flex: 1 }} />
          <Text style={s.by}>{l.listedBy === 'Owner' ? 'By owner' : 'By agency'}</Text>
        </View>

        <Text style={s.title} numberOfLines={1}>{l.title}</Text>
        <Text style={s.where}>{l.locality}, {l.city}</Text>

        <View style={s.specs}>
          <Text style={s.spec}>{l.bhk} BHK</Text>
          <View style={s.dot} />
          <Text style={s.spec}>{l.sqft} sq ft</Text>
          <View style={s.dot} />
          <Text style={s.spec}>{l.furnishing}</Text>
        </View>

        <View style={s.foot}>
          <Text style={s.deposit}>Deposit {inr(l.depositPaise, { noPaise: true })}</Text>
          <View style={{ flex: 1 }} />
          <Text style={s.expiry}>{l.daysLeft} days left</Text>
        </View>
      </View>
    </Pressable>
  );
}

const s = StyleSheet.create({
  card: {
    backgroundColor: '#FFF', borderRadius: radius.lg, overflow: 'hidden',
    marginHorizontal: space(4), marginBottom: space(3), ...shadow.card,
  },
  imgWrap: { height: 190, backgroundColor: '#DEE6EF' },
  img: { width: '100%', height: '100%' },
  badges: { position: 'absolute', top: 12, left: 12, flexDirection: 'row', gap: 6 },
  rentTag: {
    position: 'absolute', left: 12, bottom: 12,
    backgroundColor: 'rgba(255,255,255,.94)', borderRadius: radius.md,
    paddingHorizontal: 12, paddingVertical: 7,
  },
  rent: { ...font.h3, color: color.inkStrong },
  rentSub: { ...font.small, color: color.inkSoft, fontSize: 11 },

  verify: { ...font.small, color: color.positive, fontWeight: '700' },
  by: { ...font.small, color: color.inkSoft },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(2) },
  where: { ...font.small, color: color.inkSoft, marginTop: 3 },
  specs: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: space(3) },
  spec: { ...font.small, color: color.ink },
  dot: { width: 3, height: 3, borderRadius: 2, backgroundColor: color.inkFaint },
  foot: { flexDirection: 'row', alignItems: 'center', marginTop: space(3), paddingTop: space(3), borderTopWidth: 1, borderTopColor: '#EEF2F6' },
  deposit: { ...font.small, color: color.inkSoft },
  expiry: { ...font.small, color: color.inkFaint },
});
