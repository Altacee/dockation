import { createContext, useContext, useReducer, type ReactNode } from 'react';
import type {
  Worker,
  WorkerResources,
  WorkerContainer,
  WorkerImage,
  WorkerVolume,
  WorkerNetwork,
} from '../../types';

export type MigrationMode = 'cold' | 'warm' | 'live';
export type MigrationStrategy = 'full' | 'incremental' | 'snapshot';
export type TransferMode = 'direct' | 'proxy' | 'auto';

export interface SelectedResources {
  containers: WorkerContainer[];
  images: WorkerImage[];
  volumes: WorkerVolume[];
  networks: WorkerNetwork[];
}

export interface MigrationOptions {
  mode: MigrationMode;
  strategy: MigrationStrategy;
  transferMode: TransferMode;
}

interface MigrationWizardState {
  currentStep: number;
  sourceWorker: Worker | null;
  targetWorker: Worker | null;
  sourceResources: WorkerResources | null;
  selectedResources: SelectedResources;
  options: MigrationOptions;
}

type MigrationAction =
  | { type: 'SET_STEP'; step: number }
  | { type: 'SET_SOURCE_WORKER'; worker: Worker }
  | { type: 'SET_TARGET_WORKER'; worker: Worker }
  | { type: 'SET_SOURCE_RESOURCES'; resources: WorkerResources }
  | { type: 'TOGGLE_CONTAINER'; container: WorkerContainer }
  | { type: 'TOGGLE_IMAGE'; image: WorkerImage }
  | { type: 'TOGGLE_VOLUME'; volume: WorkerVolume }
  | { type: 'TOGGLE_NETWORK'; network: WorkerNetwork }
  | { type: 'SELECT_ALL_CONTAINERS' }
  | { type: 'DESELECT_ALL_CONTAINERS' }
  | { type: 'SELECT_ALL_IMAGES' }
  | { type: 'DESELECT_ALL_IMAGES' }
  | { type: 'SELECT_ALL_VOLUMES' }
  | { type: 'DESELECT_ALL_VOLUMES' }
  | { type: 'SELECT_ALL_NETWORKS' }
  | { type: 'DESELECT_ALL_NETWORKS' }
  | { type: 'SET_OPTIONS'; options: Partial<MigrationOptions> }
  | { type: 'NEXT_STEP' }
  | { type: 'PREV_STEP' }
  | { type: 'RESET' };

const initialState: MigrationWizardState = {
  currentStep: 1,
  sourceWorker: null,
  targetWorker: null,
  sourceResources: null,
  selectedResources: {
    containers: [],
    images: [],
    volumes: [],
    networks: [],
  },
  options: {
    mode: 'cold',
    strategy: 'full',
    transferMode: 'direct',
  },
};

function migrationReducer(
  state: MigrationWizardState,
  action: MigrationAction
): MigrationWizardState {
  switch (action.type) {
    case 'SET_STEP':
      return { ...state, currentStep: action.step };

    case 'SET_SOURCE_WORKER':
      return {
        ...state,
        sourceWorker: action.worker,
        sourceResources: null,
        selectedResources: initialState.selectedResources,
      };

    case 'SET_TARGET_WORKER':
      return { ...state, targetWorker: action.worker };

    case 'SET_SOURCE_RESOURCES':
      return { ...state, sourceResources: action.resources };

    case 'TOGGLE_CONTAINER': {
      const exists = state.selectedResources.containers.some(
        (c) => c.id === action.container.id
      );
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          containers: exists
            ? state.selectedResources.containers.filter((c) => c.id !== action.container.id)
            : [...state.selectedResources.containers, action.container],
        },
      };
    }

    case 'TOGGLE_IMAGE': {
      const exists = state.selectedResources.images.some((i) => i.id === action.image.id);
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          images: exists
            ? state.selectedResources.images.filter((i) => i.id !== action.image.id)
            : [...state.selectedResources.images, action.image],
        },
      };
    }

    case 'TOGGLE_VOLUME': {
      const exists = state.selectedResources.volumes.some(
        (v) => v.name === action.volume.name
      );
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          volumes: exists
            ? state.selectedResources.volumes.filter((v) => v.name !== action.volume.name)
            : [...state.selectedResources.volumes, action.volume],
        },
      };
    }

    case 'TOGGLE_NETWORK': {
      const exists = state.selectedResources.networks.some(
        (n) => n.id === action.network.id
      );
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          networks: exists
            ? state.selectedResources.networks.filter((n) => n.id !== action.network.id)
            : [...state.selectedResources.networks, action.network],
        },
      };
    }

    case 'SELECT_ALL_CONTAINERS':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          containers: state.sourceResources?.containers || [],
        },
      };

    case 'DESELECT_ALL_CONTAINERS':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          containers: [],
        },
      };

    case 'SELECT_ALL_IMAGES':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          images: state.sourceResources?.images || [],
        },
      };

    case 'DESELECT_ALL_IMAGES':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          images: [],
        },
      };

    case 'SELECT_ALL_VOLUMES':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          volumes: state.sourceResources?.volumes || [],
        },
      };

    case 'DESELECT_ALL_VOLUMES':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          volumes: [],
        },
      };

    case 'SELECT_ALL_NETWORKS':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          networks: state.sourceResources?.networks || [],
        },
      };

    case 'DESELECT_ALL_NETWORKS':
      return {
        ...state,
        selectedResources: {
          ...state.selectedResources,
          networks: [],
        },
      };

    case 'SET_OPTIONS':
      return {
        ...state,
        options: { ...state.options, ...action.options },
      };

    case 'NEXT_STEP':
      return { ...state, currentStep: Math.min(state.currentStep + 1, 5) };

    case 'PREV_STEP':
      return { ...state, currentStep: Math.max(state.currentStep - 1, 1) };

    case 'RESET':
      return initialState;

    default:
      return state;
  }
}

interface MigrationContextValue {
  state: MigrationWizardState;
  dispatch: React.Dispatch<MigrationAction>;
  // Convenience helpers
  setStep: (step: number) => void;
  nextStep: () => void;
  prevStep: () => void;
  setSourceWorker: (worker: Worker) => void;
  setTargetWorker: (worker: Worker) => void;
  setSourceResources: (resources: WorkerResources) => void;
  toggleContainer: (container: WorkerContainer) => void;
  toggleImage: (image: WorkerImage) => void;
  toggleVolume: (volume: WorkerVolume) => void;
  toggleNetwork: (network: WorkerNetwork) => void;
  selectAllContainers: () => void;
  deselectAllContainers: () => void;
  selectAllImages: () => void;
  deselectAllImages: () => void;
  selectAllVolumes: () => void;
  deselectAllVolumes: () => void;
  selectAllNetworks: () => void;
  deselectAllNetworks: () => void;
  setOptions: (options: Partial<MigrationOptions>) => void;
  reset: () => void;
  getTotalSelectedCount: () => number;
  canProceedToNextStep: () => boolean;
}

const MigrationContext = createContext<MigrationContextValue | null>(null);

export function MigrationProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(migrationReducer, initialState);

  const value: MigrationContextValue = {
    state,
    dispatch,
    setStep: (step) => dispatch({ type: 'SET_STEP', step }),
    nextStep: () => dispatch({ type: 'NEXT_STEP' }),
    prevStep: () => dispatch({ type: 'PREV_STEP' }),
    setSourceWorker: (worker) => dispatch({ type: 'SET_SOURCE_WORKER', worker }),
    setTargetWorker: (worker) => dispatch({ type: 'SET_TARGET_WORKER', worker }),
    setSourceResources: (resources) => dispatch({ type: 'SET_SOURCE_RESOURCES', resources }),
    toggleContainer: (container) => dispatch({ type: 'TOGGLE_CONTAINER', container }),
    toggleImage: (image) => dispatch({ type: 'TOGGLE_IMAGE', image }),
    toggleVolume: (volume) => dispatch({ type: 'TOGGLE_VOLUME', volume }),
    toggleNetwork: (network) => dispatch({ type: 'TOGGLE_NETWORK', network }),
    selectAllContainers: () => dispatch({ type: 'SELECT_ALL_CONTAINERS' }),
    deselectAllContainers: () => dispatch({ type: 'DESELECT_ALL_CONTAINERS' }),
    selectAllImages: () => dispatch({ type: 'SELECT_ALL_IMAGES' }),
    deselectAllImages: () => dispatch({ type: 'DESELECT_ALL_IMAGES' }),
    selectAllVolumes: () => dispatch({ type: 'SELECT_ALL_VOLUMES' }),
    deselectAllVolumes: () => dispatch({ type: 'DESELECT_ALL_VOLUMES' }),
    selectAllNetworks: () => dispatch({ type: 'SELECT_ALL_NETWORKS' }),
    deselectAllNetworks: () => dispatch({ type: 'DESELECT_ALL_NETWORKS' }),
    setOptions: (options) => dispatch({ type: 'SET_OPTIONS', options }),
    reset: () => dispatch({ type: 'RESET' }),
    getTotalSelectedCount: () => {
      const { containers, images, volumes, networks } = state.selectedResources;
      return containers.length + images.length + volumes.length + networks.length;
    },
    canProceedToNextStep: () => {
      switch (state.currentStep) {
        case 1:
          return state.sourceWorker !== null;
        case 2:
          return value.getTotalSelectedCount() > 0;
        case 3:
          return state.targetWorker !== null;
        case 4:
          return true; // Options always have defaults
        case 5:
          return true; // Ready to start
        default:
          return false;
      }
    },
  };

  return <MigrationContext.Provider value={value}>{children}</MigrationContext.Provider>;
}

export function useMigrationContext() {
  const context = useContext(MigrationContext);
  if (!context) {
    throw new Error('useMigrationContext must be used within a MigrationProvider');
  }
  return context;
}
